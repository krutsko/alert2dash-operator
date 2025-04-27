package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"sort"
	"strconv"
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
}

// ConfigMapManager handles ConfigMap operations
type ConfigMapManager interface {
	CreateOrUpdateConfigMap(ctx context.Context, dashboard *monitoringv1alpha1.AlertDashboard, content []byte) error
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
	log.Info("starting reconciliation")

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
	log := r.Log.WithValues("alertdashboard", dashboard.Name, "namespace", dashboard.Namespace)

	// Check if finalizer is present
	if !utils.HasString(dashboard.Finalizers, dashboardFinalizer) {
		return ctrl.Result{}, nil
	}

	// custom cleanup logic is not needed, because we have finalizer

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

	log := r.Log.WithValues("alertdashboard", dashboard.Name, "namespace", dashboard.Namespace)

	// Get matching PrometheusRules
	log.V(1).Info("Fetching matching PrometheusRules")
	rules, err := r.ruleManager.GetPrometheusRules(ctx, dashboard)
	if err != nil {
		log.Error(err, "Failed to list PrometheusRules")
		return fmt.Errorf("failed to list PrometheusRules: %w", err)
	}

	if len(rules) == 0 {
		log.Info("No matching PrometheusRules found")
		// Check if dashboard previously had rules
		if len(dashboard.Status.ObservedRules) > 0 {
			log.Info("Cleaning up dashboard as it no longer has matching rules")
			// Create empty dashboard or delete existing ConfigMap
			if err := r.configMapManager.CreateOrUpdateConfigMap(ctx, dashboard, []byte{}); err != nil {
				log.Error(err, "Failed to update ConfigMap")
				return fmt.Errorf("failed to update ConfigMap: %w", err)
			}
			// Update status to reflect no rules
			if err := r.updateDashboardStatus(ctx, dashboard, []monitoringv1.PrometheusRule{}); err != nil {
				log.Error(err, "Failed to update dashboard status")
				return fmt.Errorf("failed to update status: %w", err)
			}
		}
		return nil
	}
	log.V(1).Info("Found matching PrometheusRules", "count", len(rules))

	// check if PrometheusRules rules hash changed for this dashboard , if not, skip update
	ruleHash := computeRulesHash(rules)
	if dashboard.Status.RulesHash == ruleHash {
		r.Log.V(1).Info("Rules hash unchanged, skipping update",
			"dashboard", dashboard.Name,
			"namespace", dashboard.Namespace)
		return nil
	}

	// Extract grafana panel queries from rules
	log.Info("Extracting grafana panel queries from rules")
	grafanaPanelQueries := r.extractGrafanaPanelQueries(dashboard, rules)
	log.Info("Extracted grafana panel queries", "count", len(grafanaPanelQueries))

	if len(grafanaPanelQueries) == 0 {
		log.Info("No PrometheusRule found matching dashboard labels, skipping dashboard generation")
		return nil
	}

	// Generate dashboard content
	log.Info("Generating dashboard content")
	content, err := r.dashboardGen.GenerateDashboard(dashboard, grafanaPanelQueries)
	if err != nil {
		log.Error(err, "Failed to generate dashboard content")
		return fmt.Errorf("failed to generate dashboard: %w", err)
	}

	// Create or update ConfigMap
	if err := r.configMapManager.CreateOrUpdateConfigMap(ctx, dashboard, content); err != nil {
		log.Error(err, "Failed to create/update ConfigMap")
		return fmt.Errorf("failed to create/update ConfigMap: %w", err)
	}

	// Update status
	if err := r.updateDashboardStatus(ctx, dashboard, rules); err != nil {
		log.Error(err, "Failed to update dashboard status")
		return fmt.Errorf("failed to update status: %w", err)
	}

	log.V(1).Info("Successfully processed dashboard")
	return nil
}

// extractGrafanaPanelQueries extracts metrics information from PrometheusRules
func (r *AlertDashboardReconciler) extractGrafanaPanelQueries(dashboard *monitoringv1alpha1.AlertDashboard, prometheusRules []monitoringv1.PrometheusRule) []model.GrafanaPanelQuery {
	var grafanaPanelQueries []model.GrafanaPanelQuery

	// sort rules by name for deterministic order
	sort.Slice(prometheusRules, func(i, j int) bool {
		return prometheusRules[i].Name < prometheusRules[j].Name
	})

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
					parsedQueries := r.extractQuery(rule.Expr.String())
					for _, query := range parsedQueries {
						metric := model.GrafanaPanelQuery{
							Name:      rule.Alert,
							Query:     query.Query,
							Threshold: query.Threshold,
							Operator:  query.Operator,
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
	log := r.Log.WithValues("alertdashboard", dashboard.Name, "namespace", dashboard.Namespace)
	log.Info("Updating dashboard status")
	observedRules := make([]string, 0, len(rules))
	for _, rule := range rules {
		observedRules = append(observedRules, rule.Name)
	}

	// Format ConfigMap name
	configMapName := fmt.Sprintf("%s-%s", dashboard.Spec.DashboardConfig.ConfigMapNamePrefix, dashboard.Name)

	// Generate a hash that represents the combined state of all rules
	ruleHash := computeRulesHash(rules)

	// Check if status actually changed
	if dashboard.Status.ConfigMapName == configMapName &&
		dashboard.Status.RulesHash == ruleHash {

		r.Log.V(1).Info("Status unchanged, skipping update",
			"dashboard", dashboard.Name,
			"namespace", dashboard.Namespace)
		return nil
	}

	// Status is different, update it
	dashboard.Status.ConfigMapName = configMapName
	dashboard.Status.LastUpdated = time.Now().Format(time.RFC3339)
	dashboard.Status.ObservedRules = observedRules
	dashboard.Status.RulesHash = ruleHash

	return r.Status().Update(ctx, dashboard)
}

func computeRulesHash(rules []monitoringv1.PrometheusRule) string {
	// Use a hasher that can be written to
	h := sha256.New()

	// Sort rules by name for consistency
	sortedRules := make([]monitoringv1.PrometheusRule, len(rules))
	copy(sortedRules, rules)
	sort.Slice(sortedRules, func(i, j int) bool {
		return sortedRules[i].Name < sortedRules[j].Name
	})

	for _, rule := range sortedRules {
		// Simply use the ResourceVersion of each rule
		// ResourceVersion changes whenever any part of the rule changes
		h.Write([]byte(rule.Name + ":" + rule.ResourceVersion))
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

func (r *AlertDashboardReconciler) extractQuery(expr string) []model.ParsedQueryResult {

	// Parse the PromQL expression
	parsedExpr, err := parser.ParseExpr(expr)
	if err != nil {
		r.Log.Error(err, "Failed to parse PromQL expression", "expr", expr)
		return []model.ParsedQueryResult{}
	}

	var results []model.ParsedQueryResult

	switch e := parsedExpr.(type) {
	case *parser.ParenExpr:
		// extract queries from the inner parentheses
		q := r.extractQuery(e.Expr.String())
		results = append(results, q...)
	case *parser.BinaryExpr:
		switch e.Op {
		case parser.ItemType(parser.LAND), parser.ItemType(parser.LOR), parser.ItemType(parser.LUNLESS):
			// Skip logical operators entirely
			return []model.ParsedQueryResult{}
		default:
			// For comparison operators, check if either side is a scalar
			if isComparisonOperator(e.Op) {
				_, lhsIsScalar := e.LHS.(*parser.NumberLiteral)
				_, rhsIsScalar := e.RHS.(*parser.NumberLiteral)

				if !lhsIsScalar && !rhsIsScalar {
					// Neither side is a scalar, skip this query
					return []model.ParsedQueryResult{}
				}

				// Return the non-scalar side
				if rhsIsScalar {
					results = append(results, r.sanitizeExpr(e.LHS.String(), e.RHS.String(), e.Op))
				} else {
					results = append(results, r.sanitizeExpr(e.RHS.String(), e.LHS.String(), r.inverse(e.Op)))
				}
			} else {
				// skip other operators (like arithmetic)
				return []model.ParsedQueryResult{}
			}
		}
	default:
		// skip if no operator
		return []model.ParsedQueryResult{}
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

func (r *AlertDashboardReconciler) getGrafanaThresholdOperator(op parser.ItemType) string {
	switch op {
	case parser.EQL, parser.NEQ, parser.GTR, parser.GTE, parser.EQLC:
		return "gt"
	case parser.LTE, parser.LSS:
		return "lt"
	default:
		r.Log.Error(fmt.Errorf("unsupported operator: %s", op), "Unsupported operator fallback to gt")
		return "gt"
	}
}

func (r *AlertDashboardReconciler) sanitizeExpr(expr string, threshold string, operator parser.ItemType) model.ParsedQueryResult {
	// Remove surrounding parentheses if they exist
	expr = strings.TrimSpace(expr)
	for len(expr) > 2 && expr[0] == '(' && expr[len(expr)-1] == ')' {
		expr = strings.TrimSpace(expr[1 : len(expr)-1])
	}

	thresholdFloat, err := strconv.ParseFloat(threshold, 64)
	if err != nil {
		r.Log.Error(err, "Failed to parse threshold", "threshold", threshold)
		return model.ParsedQueryResult{}
	}

	return model.ParsedQueryResult{
		Query:     expr,
		Threshold: thresholdFloat,
		Operator:  r.getGrafanaThresholdOperator(operator),
	}
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
		Owns(&corev1.ConfigMap{}).
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

func (r *AlertDashboardReconciler) inverse(op parser.ItemType) parser.ItemType {
	switch op {
	case parser.GTR:
		return parser.LSS
	case parser.GTE:
		return parser.LTE
	case parser.LSS:
		return parser.GTR
	case parser.LTE:
		return parser.GTE
	default:
		return op
	}
}
