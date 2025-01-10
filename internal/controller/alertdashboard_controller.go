package controller

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-logr/logr"
	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	"github.com/krutsko/alert2dash-operator/internal/constants"
	templates "github.com/krutsko/alert2dash-operator/internal/embedfs"
	"github.com/krutsko/alert2dash-operator/internal/model"
	"github.com/krutsko/alert2dash-operator/internal/utils"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/prometheus/prometheus/promql/parser"
	corev1 "k8s.io/api/core/v1"
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

// DashboardGenerator handles dashboard template processing and generation
type DashboardGenerator interface {
	GenerateDashboard(dashboard *monitoringv1alpha1.AlertDashboard, metrics []model.GrafanaPanelQuery) ([]byte, error)
}

// RuleManager handles PrometheusRule operations
type RuleManager interface {
	GetPrometheusRules(ctx context.Context, dashboard *monitoringv1alpha1.AlertDashboard) ([]monitoringv1.PrometheusRule, error)
	FindAffectedDashboards(ctx context.Context, rule *monitoringv1.PrometheusRule) ([]monitoringv1alpha1.AlertDashboard, error)
	MatchesLabels(rule *monitoringv1.PrometheusRule, dashboard *monitoringv1alpha1.AlertDashboard) bool
}

// ConfigMapManager handles ConfigMap operations
type ConfigMapManager interface {
	CreateOrUpdateConfigMap(ctx context.Context, dashboard *monitoringv1alpha1.AlertDashboard, content []byte) error
	DeleteConfigMap(ctx context.Context, namespacedName types.NamespacedName) error
}

// AlertDashboardReconciler reconciles a AlertDashboard object
type AlertDashboardReconciler struct {
	client.Client
	Log               logr.Logger
	Scheme            *runtime.Scheme
	lastUpdated       map[types.NamespacedName]time.Time
	dashboardGen      DashboardGenerator
	ruleManager       RuleManager
	configMapManager  ConfigMapManager
	processingTimeout time.Duration
}

// NewAlertDashboardReconciler creates a new reconciler with default implementations
func NewAlertDashboardReconciler(client client.Client, scheme *runtime.Scheme, log logr.Logger) *AlertDashboardReconciler {
	return &AlertDashboardReconciler{
		Client:            client,
		Scheme:            scheme,
		Log:               log,
		lastUpdated:       make(map[types.NamespacedName]time.Time),
		dashboardGen:      &defaultDashboardGenerator{templates: templates.GrafonnetTemplates, log: log.WithName("dashboard-generator")},
		ruleManager:       &defaultRuleManager{client: client, log: log.WithName("rule-manager")},
		configMapManager:  &defaultConfigMapManager{client: client, scheme: scheme, log: log.WithName("configmap-manager")},
		processingTimeout: 30 * time.Second,
	}
}

const dashboardFinalizer = "alert2dash.monitoring.krutsko/finalizer"

// +kubebuilder:rbac:groups=monitoring.krutsko.com,resources=alertdashboards,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.krutsko.com,resources=alertdashboards/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=monitoring.krutsko.com,resources=alertdashboards/finalizers,verbs=update
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheusrules,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps/status,verbs=get
func (r *AlertDashboardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("alertdashboard", req.NamespacedName)

	// Fetch the AlertDashboard instance
	alertDashboard := &monitoringv1alpha1.AlertDashboard{}
	if err := r.Get(ctx, req.NamespacedName, alertDashboard); err != nil {
		if kuberr.IsNotFound(err) {
			// The resource is already gone
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to fetch AlertDashboard")
		return ctrl.Result{}, err
	}

	// Check if the AlertDashboard is being deleted
	if !alertDashboard.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, alertDashboard)
	}

	// Add finalizer if it doesn't exist
	if !utils.HasString(alertDashboard.Finalizers, dashboardFinalizer) {
		alertDashboard.Finalizers = append(alertDashboard.Finalizers, dashboardFinalizer)
		if err := r.Update(ctx, alertDashboard); err != nil {
			log.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		// Return here as the update will trigger another reconciliation
		return ctrl.Result{}, nil
	}

	// Process the dashboard
	if err := r.processDashboard(ctx, alertDashboard); err != nil {
		log.Error(err, "Failed to process dashboard")
		return ctrl.Result{RequeueAfter: time.Second * 10}, err
	}

	return ctrl.Result{}, nil
}

func (r *AlertDashboardReconciler) handleDeletion(ctx context.Context, dashboard *monitoringv1alpha1.AlertDashboard) (ctrl.Result, error) {
	log := r.Log.WithValues("dashboard", dashboard.Name, "namespace", dashboard.Namespace)

	// Check if finalizer is present
	if !utils.HasString(dashboard.Finalizers, dashboardFinalizer) {
		return ctrl.Result{}, nil
	}

	// Implement cleanup logic
	if err := r.cleanupDashboardResources(ctx, dashboard); err != nil {
		log.Error(err, "Failed to cleanup dashboard resources")
		return ctrl.Result{}, err
	}

	// Remove finalizer
	dashboard.Finalizers = utils.RemoveString(dashboard.Finalizers, dashboardFinalizer)
	if err := r.Update(ctx, dashboard); err != nil {
		log.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// processDashboard handles the main dashboard processing logic
func (r *AlertDashboardReconciler) processDashboard(ctx context.Context, dashboard *monitoringv1alpha1.AlertDashboard) error {
	ctx, cancel := context.WithTimeout(ctx, r.processingTimeout)
	defer cancel()

	log := r.Log.WithValues("dashboard", dashboard.Name, "namespace", dashboard.Namespace)

	// Get matching PrometheusRules
	log.Info("Fetching matching PrometheusRules")
	rules, err := r.ruleManager.GetPrometheusRules(ctx, dashboard)
	if err != nil {
		log.Error(err, "Failed to list PrometheusRules")
		return fmt.Errorf("failed to list PrometheusRules: %w", err)
	}

	if len(rules) == 0 {
		log.Info("No matching PrometheusRules found")
		return nil
	}
	log.Info("Found matching PrometheusRules", "count", len(rules))

	// Extract grafana panel queries from rules
	log.Info("Extracting grafana panel queries from rules")
	grafanaPanelQueries := r.extractGrafanaPanelQueries(dashboard, rules)
	log.Info("Extracted grafana panel queries", "count", len(grafanaPanelQueries))

	// Generate dashboard content
	log.Info("Generating dashboard content")
	content, err := r.dashboardGen.GenerateDashboard(dashboard, grafanaPanelQueries)
	if err != nil {
		log.Error(err, "Failed to generate dashboard content")
		return fmt.Errorf("failed to generate dashboard: %w", err)
	}

	// Create or update ConfigMap
	log.Info("Creating/updating ConfigMap")
	if err := r.configMapManager.CreateOrUpdateConfigMap(ctx, dashboard, content); err != nil {
		log.Error(err, "Failed to create/update ConfigMap")
		return fmt.Errorf("failed to create/update ConfigMap: %w", err)
	}

	// Update status
	log.Info("Updating dashboard status")
	if err := r.updateDashboardStatus(ctx, dashboard, rules); err != nil {
		log.Error(err, "Failed to update dashboard status")
		return fmt.Errorf("failed to update status: %w", err)
	}

	log.Info("Successfully processed dashboard")
	return nil
}

func (r *AlertDashboardReconciler) cleanupDashboardResources(ctx context.Context, dashboard *monitoringv1alpha1.AlertDashboard) error {
	log := r.Log.WithValues("dashboard", dashboard.Name, "namespace", dashboard.Namespace)

	// Get list of all related ConfigMaps
	configMapList := &corev1.ConfigMapList{}
	listOpts := []client.ListOption{
		client.InNamespace(dashboard.Namespace),
		client.MatchingLabels{
			constants.LabelGrafanaDashboard: "1",
			constants.LabelDashboardName:    dashboard.Name,
		},
	}

	if err := r.List(ctx, configMapList, listOpts...); err != nil {
		return fmt.Errorf("failed to list ConfigMaps: %w", err)
	}

	// Delete all related ConfigMaps
	for _, cm := range configMapList.Items {
		if err := r.Delete(ctx, &cm); err != nil {
			if !kuberr.IsNotFound(err) {
				log.Error(err, "Failed to delete ConfigMap", "configmap", cm.Name)
				return err
			}
		}
		log.V(1).Info("Deleted ConfigMap", "configmap", cm.Name)
	}

	return nil
}

// extractGrafanaPanelQueries extracts metrics information from PrometheusRules
func (r *AlertDashboardReconciler) extractGrafanaPanelQueries(dashboard *monitoringv1alpha1.AlertDashboard, prometheusRules []monitoringv1.PrometheusRule) []model.GrafanaPanelQuery {
	var grafanaPanelQueries []model.GrafanaPanelQuery

	for _, rule := range prometheusRules {
		for _, group := range rule.Spec.Groups {
			for _, rule := range group.Rules {
				if rule.Alert != "" {
					// Skip if rule doesn't match dashboard's RuleLabelSelector
					if dashboard.Spec.RuleLabelSelector != nil {
						selector, err := metav1.LabelSelectorAsSelector(dashboard.Spec.RuleLabelSelector)
						if err == nil && !selector.Matches(labels.Set(rule.Labels)) {
							continue
						}
					}
					// skip rule has exclude label
					if _, excluded := rule.Labels[constants.LabelExcludeRule]; excluded {
						continue
					}
					promql := r.extractQuery(&rule)
					for _, query := range promql {
						metric := model.GrafanaPanelQuery{
							Name:  rule.Alert,
							Query: query,
						}
						grafanaPanelQueries = append(grafanaPanelQueries, metric)
					}
				}
			}
		}
	}

	return grafanaPanelQueries
}

// updateDashboardStatus updates the status of the AlertDashboard resource
func (r *AlertDashboardReconciler) updateDashboardStatus(ctx context.Context,
	dashboard *monitoringv1alpha1.AlertDashboard,
	rules []monitoringv1.PrometheusRule) error {

	observedRules := make([]string, 0, len(rules))
	for _, rule := range rules {
		observedRules = append(observedRules, rule.Name)
	}

	dashboard.Status.ConfigMapName = fmt.Sprintf("%s-%s", dashboard.Spec.DashboardConfig.ConfigMapNamePrefix, dashboard.Name)
	dashboard.Status.LastUpdated = time.Now().Format(time.RFC3339)
	dashboard.Status.ObservedRules = observedRules

	return r.Status().Update(ctx, dashboard)
}

func (r *AlertDashboardReconciler) extractQuery(alert *monitoringv1.Rule) []string {
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
	_, hasExcludeLabel := rule.Labels[constants.LabelExcludeRule]
	return !hasExcludeLabel
}

func (p *prometheusRulePredicate) Update(e event.UpdateEvent) bool {
	oldRule, ok1 := e.ObjectOld.(*monitoringv1.PrometheusRule)
	newRule, ok2 := e.ObjectNew.(*monitoringv1.PrometheusRule)
	if !ok1 || !ok2 {
		return false
	}

	// Skip if the rule has the exclude label
	for _, group := range newRule.Spec.Groups {
		for _, rule := range group.Rules {
			if _, hasExcludeLabel := rule.Labels[constants.LabelExcludeRule]; hasExcludeLabel {
				return false
			}
		}
	}

	// Only trigger updates if the spec changed
	// Ignore metadata-only changes (like resourceVersion, annotations, etc.)
	specChanged := !reflect.DeepEqual(oldRule.Spec, newRule.Spec)
	return specChanged
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
		// Log error with more context
		r.Log.Error(err, "Failed to find affected dashboards",
			"rule", rule.Name,
			"namespace", rule.Namespace)

		// Return the rule's own namespace for reprocessing
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Name:      rule.Name,
				Namespace: rule.Namespace,
			},
		}}
	}

	requests := make([]reconcile.Request, 0, len(affectedDashboards))
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
