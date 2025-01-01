/*
Copyright (c) 2024

Licensed under MIT License. See LICENSE file in the root directory of this repository.
*/

package controller

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-jsonnet"
	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	kuberr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

//go:embed templates
var templates embed.FS

// Custom importer for embedded files
type embedImporter struct {
	templates embed.FS
}

func (i *embedImporter) Import(importedFrom, importedPath string) (contents jsonnet.Contents, foundAt string, err error) {
	// If the path starts with vendor, it's relative to templates directory
	var fullPath string
	if strings.HasPrefix(importedPath, "vendor/") {
		fullPath = filepath.Join("templates", importedPath)
	} else {
		// Handle relative imports
		if importedFrom != "" {
			dir := filepath.Dir(importedFrom)
			importedPath = filepath.Join(dir, importedPath)
		}
		fullPath = filepath.Join("templates", importedPath)
	}

	// Read the file
	content, err := i.templates.ReadFile(fullPath)
	if err != nil {
		// log.Printf("Failed to read file %s: %v", fullPath, err)
		return jsonnet.Contents{}, "", err
	}

	return jsonnet.MakeContents(string(content)), importedPath, nil
}

// AlertDashboardReconciler reconciles a AlertDashboard object
type AlertDashboardReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

const dashboardTemplate = `
local dashboard = {
    new(title, rules, config):: {
        local cfg = if config == null then {} else config,
        local panels = if cfg.panels == null then {} else cfg.panels,
        
        title: title,
        editable: true,
        panels: [],
        templating: {
            list: []
        },
        time: {
            from: 'now-6h',
            to: 'now'
        },
        refresh: '1m',

        // Add overview panel if enabled
        addAlertsOverview(rules):: if panels.alertsOverview then self + {
            panels+: [{
                title: 'Alerts Overview',
                type: 'table',
                gridPos: { h: 8, w: 24, x: 0, y: 0 },
                datasource: { type: 'prometheus', uid: 'prometheus' },
                targets: [{
                    expr: 'ALERTS',
                    instant: true,
                    refId: 'A'
                }],
                transformations: [{
                    id: 'organize',
                    options: {
                        excludeByName: {},
                        indexByName: {},
                        renameByName: {
                            alertname: 'Alert',
                            alertstate: 'State',
                            severity: 'Severity'
                        }
                    }
                }]
            }]
        } else self,

        // Add time series panels if enabled
        addTimeSeriesGraphs(rules):: if panels.timeSeriesGraphs then self + {
            local ruleArray = if std.type(rules) == 'array' then rules else [],
            panels+: std.mapWithIndex(function(i, rule) {
                title: rule.alert,
                type: 'timeseries',
                gridPos: {
                    h: 8,
                    w: 12,
                    x: (i % 2) * 12,
                    y: std.floor(i / 2) * 8 + (if panels.alertsOverview then 8 else 0)
                },
                datasource: { type: 'prometheus', uid: 'prometheus' },
                description: if std.objectHas(rule, 'annotations') then rule.annotations.description else '',
                targets: [{
                    expr: rule.expr,
                    legendFormat: rule.alert,
                    refId: 'A'
                }],
                fieldConfig: {
                    defaults: {
                        custom: {
                            drawStyle: 'line',
                            lineInterpolation: 'linear',
                            spanNulls: false
                        },
                        thresholds: {
                            mode: 'absolute',
                            steps: [
                                { value: null, color: 'green' },
                                { value: 0.7, color: 'red' }
                            ]
                        }
                    }
                }
            }, ruleArray)
        } else self,

        // Add alert history if enabled
        addAlertHistory():: if panels.alertHistory then self + {
            panels+: [{
                title: 'Alert History',
                type: 'timeseries',
                gridPos: {
                    h: 8,
                    w: 24,
                    x: 0,
                    y: std.length(self.panels) * 8
                },
                datasource: { type: 'prometheus', uid: 'prometheus' },
                targets: [{
                    expr: 'changes(ALERTS{alertstate="firing"}[24h])',
                    legendFormat: '{{alertname}}',
                    refId: 'A'
                }]
            }]
        } else self,

        // Add template variables
        addVariables(vars):: self + {
            local varArray = if std.type(vars) == 'array' then vars else [],
            templating+: {
                list+: std.map(function(v)
                    if v.type == 'query' then {
                        name: v.name,
                        type: 'query',
                        datasource: { type: 'prometheus', uid: 'prometheus' },
                        query: v.query,
                        refresh: 2,
                        sort: 1
                    } else if v.type == 'custom' then {
                        name: v.name,
                        type: 'custom',
                        query: std.join(',', v.values),
                        current: { selected: true, text: v.values[0], value: v.values[0] }
                    }, varArray)
            }
        }
    }
};

local rules = std.parseJson('%s');
local config = std.parseJson('%s');
local variables = if std.length('%s') > 0 then std.parseJson('%s') else [];

dashboard.new('%s', rules, config)
    .addAlertsOverview(rules)
    .addTimeSeriesGraphs(rules)
    .addAlertHistory()
    .addVariables(variables)
`

// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheusrules,verbs=get;list;watch
// +kubebuilder:rbac:groups=monitoring.krutsko.com,resources=alertdashboards,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.krutsko.com,resources=alertdashboards/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=monitoring.krutsko.com,resources=alertdashboards/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AlertDashboard object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *AlertDashboardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithName("AlertDashboardReconciler")
	r.Log = log

	// Fetch the AlertDashboard instance
	alertDashboard := &monitoringv1alpha1.AlertDashboard{}
	if err := r.Get(ctx, req.NamespacedName, alertDashboard); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch AlertDashboard")
			return ctrl.Result{}, err
		}
		// Handle deletion
		err = r.onDashboardDeleted(ctx, req.Name)
		if err != nil {
			log.Error(err, "error handling dashboard deletion")
			return ctrl.Result{RequeueAfter: time.Second * 10}, err
		}
		return ctrl.Result{}, nil
	}

	// List all PrometheusRules that match criteria in AlertDashboard
	filteredPrometheusRules, err := r.getPrometheusRules(ctx, req, alertDashboard)

	// Check if there was an error during the listing
	if err != nil {
		log.Error(err, "unable to list PrometheusRules")
		return ctrl.Result{}, err
	}

	// Check if there are any matching PrometheusRules
	if len(filteredPrometheusRules) == 0 {
		log.Info("No matching PrometheusRules found, skipping dashboard creation")
		return ctrl.Result{}, nil
	}

	// Collect all queries from alert rules to be used to generate the dashboard
	var allQueries []map[string]interface{}
	var observedRules []string // TODO: check if this is needed, it used only in Status
	for _, rule := range filteredPrometheusRules {
		observedRules = append(observedRules, rule.Name)
		for _, group := range rule.Spec.Groups {
			for _, alertRule := range group.Rules {
				if alertRule.Alert != "" {
					// Extract the base query by removing comparison operators
					baseQuery := r.extractBaseQuery(alertRule.Expr.StrVal)

					ruleMap := map[string]interface{}{
						"name":  string(alertRule.Alert),
						"query": baseQuery,
					}
					allQueries = append(allQueries, ruleMap)
				}
			}
		}
	}

	// Convert queries to JSON
	metricsJSON, err := json.Marshal(allQueries)
	if err != nil {
		log.Error(err, "Failed to marshal metrics")
		return ctrl.Result{}, err
	}

	// create and evaluate jsonnet vm with external variables to generate the dashboard
	prettyResult, err := r.generateDashboard(alertDashboard, metricsJSON, log)
	if err != nil {
		log.Error(err, "Failed to generate dashboard")
		return ctrl.Result{}, err
	}

	// Create or update ConfigMap
	configMapName := alertDashboard.Spec.DashboardConfig.ConfigMapNamePrefix + "-" + alertDashboard.Name
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: alertDashboard.Namespace,
			Labels: map[string]string{
				"grafana_dashboard": "1",
			},
		},
	}

	if err := r.CreateOrUpdate(ctx, configMap, func() error {
		if configMap.Data == nil {
			configMap.Data = make(map[string]string)
		}
		configMap.Data[alertDashboard.Name+".json"] = string(prettyResult)
		return ctrl.SetControllerReference(alertDashboard, configMap, r.Scheme)
	}); err != nil {
		log.Error(err, "unable to create or update ConfigMap")
		return ctrl.Result{}, err
	}

	// Update status
	alertDashboard.Status.ConfigMapName = configMapName
	alertDashboard.Status.LastUpdated = time.Now().Format(time.RFC3339)
	alertDashboard.Status.ObservedRules = observedRules
	if err := r.Status().Update(ctx, alertDashboard); err != nil {
		log.Error(err, "unable to update AlertDashboard status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *AlertDashboardReconciler) generateDashboard(alertDashboard *monitoringv1alpha1.AlertDashboard, metricsJSON []byte, log logr.Logger) ([]byte, error) {

	// Create Jsonnet VM
	vm := jsonnet.MakeVM()

	// Set external variables
	vm.ExtVar("title", alertDashboard.Name)
	vm.ExtVar("metrics", string(metricsJSON))

	// Set the custom importer
	vm.Importer(&embedImporter{templates: templates})

	template, err := templates.ReadFile("templates/dashboard.jsonnet")
	if err != nil {
		log.Error(err, "Failed to read template")
		return nil, err
	}

	result, err := vm.EvaluateSnippet("dashboard.jsonnet", string(template))
	if err != nil {
		log.Error(err, "Failed to evaluate template")
		return nil, err
	}

	var prettyJSON map[string]interface{}
	if err := json.Unmarshal([]byte(result), &prettyJSON); err != nil {
		log.Error(err, "Failed to parse JSON")
		return nil, err
	}

	prettyResult, err := json.MarshalIndent(prettyJSON, "", "  ")
	if err != nil {
		log.Error(err, "Failed to format JSON")
		return nil, err
	}

	fmt.Println(string(prettyResult))
	return prettyResult, nil
}

func (r *AlertDashboardReconciler) getPrometheusRules(ctx context.Context, req reconcile.Request, alertDashboard *monitoringv1alpha1.AlertDashboard) ([]monitoringv1.PrometheusRule, error) {
	var filteredRules []monitoringv1.PrometheusRule

	ruleList := &monitoringv1.PrometheusRuleList{}

	listOptions := &client.ListOptions{
		Namespace:     req.Namespace,
		LabelSelector: labels.Set{"generate-dashboard": "true"}.AsSelector(),
	}

	err := r.List(ctx, ruleList, listOptions)

	if err != nil {
		return filteredRules, err
	}

	// Filter rules based on labels
	for _, rule := range ruleList.Items {
		if r.matchesLabels(rule, alertDashboard.Spec.RuleSelector) {
			filteredRules = append(filteredRules, *rule)
		}
	}
	return filteredRules, err
}

// Helper function for Create or Update operation
func (r *AlertDashboardReconciler) CreateOrUpdate(ctx context.Context, obj client.Object, mutate func() error) error {
	key := client.ObjectKeyFromObject(obj)
	if err := r.Get(ctx, key, obj); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		if err := mutate(); err != nil {
			return err
		}
		return r.Create(ctx, obj)
	}

	if err := mutate(); err != nil {
		return err
	}
	return r.Update(ctx, obj)
}

// SetupWithManager sets up the controller with the Manager.
func (r *AlertDashboardReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.AlertDashboard{}).
		Complete(r)
}

// matchesLabels checks if a PrometheusRule matches the given label selector
func (r *AlertDashboardReconciler) matchesLabels(rule *monitoringv1.PrometheusRule, selector *metav1.LabelSelector) bool {
	if selector == nil {
		return true
	}

	// Check matchLabels
	for k, v := range selector.MatchLabels {
		// Check resource metadata labels
		if resourceVal, ok := rule.Labels[k]; ok {
			if resourceVal != v {
				return false
			}
			continue
		}

		// Check labels in rule groups and alert rules
		matchFound := false
		for _, group := range rule.Spec.Groups {
			// Check group labels
			if groupVal, ok := group.Labels[k]; ok && groupVal == v {
				matchFound = true
				break
			}

			// Check individual alert rule labels
			for _, alertRule := range group.Rules {
				if alertVal, ok := alertRule.Labels[k]; ok && alertVal == v {
					matchFound = true
					break
				}
			}
			if matchFound {
				break
			}
		}
		if !matchFound {
			return false
		}
	}

	// Check matchExpressions
	for _, expr := range selector.MatchExpressions {
		switch expr.Operator {
		case metav1.LabelSelectorOpExists:
			exists := false
			// Check resource metadata labels
			if _, ok := rule.Labels[expr.Key]; ok {
				exists = true
			}

			// Check labels in rule groups and alert rules
			if !exists {
				for _, group := range rule.Spec.Groups {
					// Check group labels
					if _, ok := group.Labels[expr.Key]; ok {
						exists = true
						break
					}

					// Check individual alert rule labels
					for _, alertRule := range group.Rules {
						if _, ok := alertRule.Labels[expr.Key]; ok {
							exists = true
							break
						}
					}
					if exists {
						break
					}
				}
			}
			if !exists {
				return false
			}
		}
	}

	return true
}

// extractBaseQuery removes comparison operators from a Prometheus alert expression
// to get the base metric query
func (a *AlertDashboardReconciler) extractBaseQuery(expr string) string {
	// Common comparison operators in Prometheus alerts
	operators := []string{">", ">=", "<", "<=", "==", "!="}

	// Find the first occurrence of any operator and trim the rest
	query := expr
	for _, op := range operators {
		if idx := strings.Index(expr, op); idx != -1 {
			query = strings.TrimSpace(expr[:idx])
			break
		}
	}
	return query
}

// onDashboardDeleted handles cleanup when an AlertDashboard is deleted
func (r *AlertDashboardReconciler) onDashboardDeleted(ctx context.Context, name string) error {
	log := log.FromContext(ctx)

	// Find all ConfigMaps that might contain our dashboard
	configMapList := &corev1.ConfigMapList{}
	if err := r.List(ctx, configMapList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"grafana_dashboard": "1",
		}),
	}); err != nil {
		return fmt.Errorf("failed to list ConfigMaps: %w", err)
	}

	for _, cm := range configMapList.Items {
		cm := cm // Create a new variable for the closure

		// Check if this ConfigMap contains our dashboard
		if strings.HasSuffix(cm.Name, "-"+name) {
			// Delete the ConfigMap
			if err := r.Delete(ctx, &cm); err != nil {
				if !kuberr.IsNotFound(err) {
					return fmt.Errorf("failed to delete ConfigMap %s: %w", cm.Name, err)
				}
			}
			log.Info("deleted dashboard ConfigMap", "namespace", cm.Namespace, "name", cm.Name)
		}
	}

	return nil
}
