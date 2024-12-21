/*
Copyright (c) 2024

Licensed under MIT License. See LICENSE file in the root directory of this repository.
*/

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/google/go-jsonnet"
	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AlertDashboardReconciler reconciles a AlertDashboard object
type AlertDashboardReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const dashboardTemplate = `
{
    new(title, rules):: {
        title: title,
        editable: true,
        panels: std.mapWithIndex(function(i, rule) {
            title: rule.alert,
            type: 'timeseries',
            datasource: {
                type: 'prometheus',
                uid: 'prometheus'
            },
            description: if std.objectHas(rule, 'annotations') then rule.annotations.description else '',
            targets: [{
                expr: rule.expr,
                legendFormat: if std.objectHas(rule, 'labels') then std.join(' ', 
                    [std.format('%s=%s', [k, rule.labels[k]]) for k in std.objectFields(rule.labels)]) 
                else rule.alert,
                refId: 'A'
            }],
            gridPos: {
                h: 8,
                w: 12,
                x: (i % 2) * 12,
                y: std.floor(i / 2) * 8
            },
            fieldConfig: {
                defaults: {
                    custom: {
                        drawStyle: 'line',
                        lineInterpolation: 'linear',
                        spanNulls: false,
                        showPoints: 'never'
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
        }, rules),
        templating: {
            list: [{
                name: 'namespace',
                type: 'query',
                datasource: { type: 'prometheus', uid: 'prometheus' },
                refresh: 2,
                regex: '',
                sort: 1,
                query: 'label_values(namespace)'
            }]
        },
        time: {
            from: 'now-6h',
            to: 'now'
        },
        refresh: '1m'
    }
}
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
	log := log.FromContext(ctx)

	// Fetch the AlertDashboard instance
	alertDashboard := &monitoringv1alpha1.AlertDashboard{}
	if err := r.Get(ctx, req.NamespacedName, alertDashboard); err != nil {
		log.Error(err, "unable to fetch AlertDashboard")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// List PrometheusRules based on label selector
	ruleList := &monitoringv1.PrometheusRuleList{}
	selector, _ := metav1.LabelSelectorAsSelector(alertDashboard.Spec.RuleSelector)
	if err := r.List(ctx, ruleList, &client.ListOptions{
		LabelSelector: selector,
		Namespace:     req.Namespace,
	}); err != nil {
		log.Error(err, "unable to list PrometheusRules")
		return ctrl.Result{}, err
	}

	// Collect all alert rules
	var allRules []map[string]interface{}
	var observedRules []string
	for _, rule := range ruleList.Items {
		observedRules = append(observedRules, rule.Name)
		for _, group := range rule.Spec.Groups {
			for _, alertRule := range group.Rules {
				if alertRule.Alert != "" {
					ruleMap := map[string]interface{}{
						"alert":       alertRule.Alert,
						"expr":        alertRule.Expr,
						"annotations": alertRule.Annotations,
						"labels":      alertRule.Labels,
					}
					allRules = append(allRules, ruleMap)
				}
			}
		}
	}

	// Generate dashboard using Jsonnet
	vm := jsonnet.MakeVM()
	jsonStr := fmt.Sprintf(
		`%s.new("%s", %s)`,
		dashboardTemplate,
		alertDashboard.Spec.DashboardConfig.Title,
		string(mustMarshal(allRules)),
	)

	dashboardJSON, err := vm.EvaluateSnippet("", jsonStr)
	if err != nil {
		log.Error(err, "failed to evaluate Jsonnet template")
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
		configMap.Data[alertDashboard.Name+".json"] = dashboardJSON
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

func mustMarshal(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
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
