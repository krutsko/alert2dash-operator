package controller

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-logr/logr"
	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	kuberr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

//go:embed templates
var templates embed.FS

// DashboardGenerator handles dashboard template processing and generation
type DashboardGenerator interface {
	GenerateDashboard(dashboard *monitoringv1alpha1.AlertDashboard, metrics []byte) ([]byte, error)
}

// RuleManager handles PrometheusRule operations
type RuleManager interface {
	GetPrometheusRules(ctx context.Context, namespace string, labelSelector labels.Selector) ([]monitoringv1.PrometheusRule, error)
	FindAffectedDashboards(ctx context.Context, rule *monitoringv1.PrometheusRule) ([]monitoringv1alpha1.AlertDashboard, error)
	MatchesLabels(rule *monitoringv1.PrometheusRule, selector *metav1.LabelSelector) bool
}

// ConfigMapManager handles ConfigMap operations
type ConfigMapManager interface {
	CreateOrUpdateConfigMap(ctx context.Context, dashboard *monitoringv1alpha1.AlertDashboard, content []byte) error
	DeleteConfigMap(ctx context.Context, namespacedName types.NamespacedName) error
}

// AlertDashboardReconciler reconciles a AlertDashboard object
type AlertDashboardReconciler struct {
	client.Client
	Log              logr.Logger
	Scheme           *runtime.Scheme
	lastUpdated      map[types.NamespacedName]time.Time
	dashboardGen     DashboardGenerator
	ruleManager      RuleManager
	configMapManager ConfigMapManager
}

// NewAlertDashboardReconciler creates a new reconciler with default implementations
func NewAlertDashboardReconciler(client client.Client, scheme *runtime.Scheme, log logr.Logger) *AlertDashboardReconciler {
	return &AlertDashboardReconciler{
		Client:           client,
		Scheme:           scheme,
		Log:              log,
		lastUpdated:      make(map[types.NamespacedName]time.Time),
		dashboardGen:     &defaultDashboardGenerator{templates: templates, log: log.WithName("dashboard-generator")},
		ruleManager:      &defaultRuleManager{client: client, log: log.WithName("rule-manager")},
		configMapManager: &defaultConfigMapManager{client: client, scheme: scheme, log: log.WithName("configmap-manager")},
	}
}

// Reconcile handles the reconciliation of AlertDashboard resources
func (r *AlertDashboardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("alertdashboard", req.NamespacedName)

	// Rate limiting check
	if r.shouldSkipReconciliation(req.NamespacedName) {
		log.V(1).Info("Skipping reconciliation - too soon since last update")
		return ctrl.Result{}, nil
	}

	// Fetch the AlertDashboard instance
	alertDashboard := &monitoringv1alpha1.AlertDashboard{}
	if err := r.Get(ctx, req.NamespacedName, alertDashboard); err != nil {
		if kuberr.IsNotFound(err) {
			return r.handleDashboardDeletion(ctx, req.NamespacedName)
		}
		log.Error(err, "Failed to fetch AlertDashboard")
		return ctrl.Result{}, err
	}

	// Process the dashboard
	if err := r.processDashboard(ctx, alertDashboard); err != nil {
		log.Error(err, "Failed to process dashboard")
		return ctrl.Result{RequeueAfter: time.Second * 10}, err
	}

	return ctrl.Result{}, nil
}

// shouldSkipReconciliation implements rate limiting logic
func (r *AlertDashboardReconciler) shouldSkipReconciliation(namespacedName types.NamespacedName) bool {
	lastUpdate, exists := r.lastUpdated[namespacedName]
	if exists && time.Since(lastUpdate) < 5*time.Second {
		return true
	}
	r.lastUpdated[namespacedName] = time.Now()
	return false
}

// processDashboard handles the main dashboard processing logic
func (r *AlertDashboardReconciler) processDashboard(ctx context.Context, dashboard *monitoringv1alpha1.AlertDashboard) error {
	// Get matching PrometheusRules
	rules, err := r.ruleManager.GetPrometheusRules(ctx, dashboard.Namespace,
		labels.Set{"generate-dashboard": "true"}.AsSelector())
	if err != nil {
		return fmt.Errorf("failed to list PrometheusRules: %w", err)
	}

	if len(rules) == 0 {
		r.Log.Info("No matching PrometheusRules found", "dashboard", dashboard.Name)
		return nil
	}

	// Extract metrics from rules
	metrics, err := r.extractMetrics(rules)
	if err != nil {
		return fmt.Errorf("failed to extract metrics: %w", err)
	}

	// Generate dashboard content
	content, err := r.dashboardGen.GenerateDashboard(dashboard, metrics)
	if err != nil {
		return fmt.Errorf("failed to generate dashboard: %w", err)
	}

	// Create or update ConfigMap
	if err := r.configMapManager.CreateOrUpdateConfigMap(ctx, dashboard, content); err != nil {
		return fmt.Errorf("failed to create/update ConfigMap: %w", err)
	}

	// Update status
	if err := r.updateDashboardStatus(ctx, dashboard, rules); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	return nil
}

// handleDashboardDeletion handles cleanup when a dashboard is deleted
func (r *AlertDashboardReconciler) handleDashboardDeletion(ctx context.Context, namespacedName types.NamespacedName) (ctrl.Result, error) {
	if err := r.configMapManager.DeleteConfigMap(ctx, namespacedName); err != nil {
		r.Log.Error(err, "Failed to delete ConfigMap")
		return ctrl.Result{RequeueAfter: time.Second * 10}, err
	}
	return ctrl.Result{}, nil
}

// extractMetrics extracts metrics information from PrometheusRules
func (r *AlertDashboardReconciler) extractMetrics(rules []monitoringv1.PrometheusRule) ([]byte, error) {
	var allQueries []map[string]interface{}

	for _, rule := range rules {
		for _, group := range rule.Spec.Groups {
			for _, alertRule := range group.Rules {
				if alertRule.Alert != "" {
					baseQuery := r.extractBaseQuery(&alertRule)
					ruleMap := map[string]interface{}{}
					for _, query := range baseQuery {
						ruleMap["name"] = string(alertRule.Alert)
						ruleMap["query"] = query
					}
					allQueries = append(allQueries, ruleMap)
				}
			}
		}
	}

	return json.Marshal(allQueries)
}

// updateDashboardStatus updates the status of the AlertDashboard resource
func (r *AlertDashboardReconciler) updateDashboardStatus(ctx context.Context,
	dashboard *monitoringv1alpha1.AlertDashboard,
	rules []monitoringv1.PrometheusRule) error {

	var observedRules []string
	for _, rule := range rules {
		observedRules = append(observedRules, rule.Name)
	}

	dashboard.Status.ConfigMapName = fmt.Sprintf("%s-%s", dashboard.Spec.DashboardConfig.ConfigMapNamePrefix, dashboard.Name)
	dashboard.Status.LastUpdated = time.Now().Format(time.RFC3339)
	dashboard.Status.ObservedRules = observedRules

	return r.Status().Update(ctx, dashboard)
}

// extractBaseQuery removes comparison operators from a Prometheus alert expression
func (r *AlertDashboardReconciler) extractBaseQuery(alert *monitoringv1.Rule) []string {
	operators := []string{">", ">=", "<", "<=", "==", "!="}
	query := alert.Expr.String()
	var queries []string

	// Handle logical operators
	for _, logicalOp := range []string{" and ", " or "} { // todo " unless "?
		if strings.Contains(query, logicalOp) {
			parts := strings.Split(query, logicalOp)
			// Process each part of the expression
			for _, part := range parts {
				cleanQuery := r.cleanQueryPart(strings.TrimSpace(part), operators)
				if cleanQuery != "" {
					queries = append(queries, cleanQuery)
				}
			}
			return queries
		}
	}

	// If no logical operators, process the single query
	cleanQuery := r.cleanQueryPart(query, operators)
	return []string{cleanQuery}
}

// cleanQueryPart removes comparison operators and bool modifiers from a query part
func (r *AlertDashboardReconciler) cleanQueryPart(query string, operators []string) string {
	// Remove outer parentheses
	query = strings.TrimSpace(query)
	if strings.HasPrefix(query, "(") && strings.HasSuffix(query, ")") {
		query = query[1 : len(query)-1]
	}

	// Remove comparison operators and bool modifier
	for _, op := range operators {
		if idx := strings.Index(query, op+" bool"); idx != -1 {
			query = strings.TrimSpace(query[:idx])
			break
		}
		if idx := strings.Index(query, op); idx != -1 {
			query = strings.TrimSpace(query[:idx])
			break
		}
	}

	return query
}

// PrometheusRulePredicate filters PrometheusRule events
type prometheusRulePredicate struct {
	predicate.Funcs
}

func (p *prometheusRulePredicate) Create(e event.CreateEvent) bool {
	rule, ok := e.Object.(*monitoringv1.PrometheusRule)
	if !ok {
		return false
	}
	return rule.Labels["generate-dashboard"] == "true"
}

func (p *prometheusRulePredicate) Update(e event.UpdateEvent) bool {
	oldRule, ok1 := e.ObjectOld.(*monitoringv1.PrometheusRule)
	newRule, ok2 := e.ObjectNew.(*monitoringv1.PrometheusRule)
	if !ok1 || !ok2 {
		return false
	}

	// Only trigger if the rule has the required label and specs have changed
	if newRule.Labels["generate-dashboard"] != "true" {
		return false
	}

	// Compare the specs to see if anything relevant changed
	return !reflect.DeepEqual(oldRule.Spec, newRule.Spec)
}

// SetupWithManager sets up the controller with the Manager
func (r *AlertDashboardReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.AlertDashboard{}).
		Watches(
			&monitoringv1.PrometheusRule{},
			handler.EnqueueRequestsFromMapFunc(r.handlePrometheusRuleEvent),
			builder.WithPredicates(&prometheusRulePredicate{}),
		).
		Complete(r)
}

// handlePrometheusRuleEvent handles PrometheusRule events
func (r *AlertDashboardReconciler) handlePrometheusRuleEvent(ctx context.Context, obj client.Object) []reconcile.Request {
	rule, ok := obj.(*monitoringv1.PrometheusRule)
	if !ok {
		return nil
	}

	affectedDashboards, err := r.ruleManager.FindAffectedDashboards(ctx, rule)
	if err != nil {
		r.Log.Error(err, "Failed to find affected dashboards")
		return nil
	}

	var requests []reconcile.Request
	for _, dashboard := range affectedDashboards {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      dashboard.Name,
				Namespace: dashboard.Namespace,
			},
		})
	}

	return requests
}
