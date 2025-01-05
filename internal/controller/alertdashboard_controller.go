package controller

import (
	"context"
	"embed"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-logr/logr"
	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	"github.com/krutsko/alert2dash-operator/internal/model"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/prometheus/prometheus/promql/parser"
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
	GenerateDashboard(dashboard *monitoringv1alpha1.AlertDashboard, metrics []model.AlertMetric) ([]byte, error)
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
func (r *AlertDashboardReconciler) extractMetrics(rules []monitoringv1.PrometheusRule) ([]model.AlertMetric, error) {
	var metrics []model.AlertMetric

	for _, rule := range rules {
		for _, group := range rule.Spec.Groups {
			for _, alertRule := range group.Rules {
				if alertRule.Alert != "" {
					baseQueries := r.extractBaseQuery(&alertRule)
					for _, query := range baseQueries {
						metric := model.AlertMetric{
							Name:  string(alertRule.Alert),
							Query: query,
						}
						metrics = append(metrics, metric)
					}
				}
			}
		}
	}

	return metrics, nil
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

func (r *AlertDashboardReconciler) extractBaseQuery(alert *monitoringv1.Rule) []string {
	expr := alert.Expr.String()

	// Parse the PromQL expression
	parsedExpr, err := parser.ParseExpr(expr)
	if err != nil {
		r.Log.Error(err, "Failed to parse PromQL expression", "expr", expr)
		return []string{""}
	}

	var results []string

	switch e := parsedExpr.(type) {
	case *parser.ParenExpr:
		// For parenthesized expressions, process the inner expression
		expr := e.Expr.String()
		results = append(results, sanitizeExpr(expr))
	case *parser.BinaryExpr:
		switch e.Op {
		case parser.ItemType(parser.LAND), parser.ItemType(parser.LOR), parser.ItemType(parser.LUNLESS):
			// Skip logical operators entirely
			return []string{}
		default:
			// For comparison operators, just take the left side
			if isComparisonOperator(e.Op) {
				results = append(results, sanitizeExpr(e.LHS.String()))
			} else {
				// For other operators (like arithmetic), keep the whole expression
				results = append(results, sanitizeExpr(parsedExpr.String()))
			}
		}
	default:
		// If no operator, return the expression as is
		results = append(results, sanitizeExpr(parsedExpr.String()))
	}

	return results
}

func isComparisonOperator(op parser.ItemType) bool {
	switch op {
	case parser.EQL, parser.NEQ, parser.GTR, parser.LSS, parser.GTE, parser.LTE, parser.EQLC:
		return true
	}
	return false
}

func sanitizeExpr(expr string) string {
	// Remove surrounding parentheses if they exist
	expr = strings.TrimSpace(expr)
	for len(expr) > 2 && expr[0] == '(' && expr[len(expr)-1] == ')' {
		expr = strings.TrimSpace(expr[1 : len(expr)-1])
	}
	return expr
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
