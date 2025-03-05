package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	"github.com/krutsko/alert2dash-operator/internal/constants"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// defaultRuleManager implements RuleManager
type defaultRuleManager struct {
	client client.Client
	log    logr.Logger
}

func (m *defaultRuleManager) GetPrometheusRules(ctx context.Context, dashboard *monitoringv1alpha1.AlertDashboard) ([]monitoringv1.PrometheusRule, error) {
	ruleList := &monitoringv1.PrometheusRuleList{}
	var labelSelector labels.Selector
	if dashboard.Spec.MetadataLabelSelector != nil {
		labelSelector = labels.SelectorFromSet(dashboard.Spec.MetadataLabelSelector.MatchLabels)
	} else {
		labelSelector = labels.Everything()
	}
	listOpts := &client.ListOptions{
		Namespace:     dashboard.Namespace,
		LabelSelector: labelSelector,
	}

	if err := m.client.List(ctx, ruleList, listOpts); err != nil {
		return nil, fmt.Errorf("failed to list PrometheusRules: %w", err)
	}

	rules := make([]monitoringv1.PrometheusRule, 0, len(ruleList.Items))
	for _, rule := range ruleList.Items {
		rules = append(rules, *rule)
	}

	m.log.V(1).Info("Found PrometheusRules", "count", len(ruleList.Items))
	return rules, nil
}

func (m *defaultRuleManager) FindAffectedDashboards(ctx context.Context, rule *monitoringv1.PrometheusRule) ([]monitoringv1alpha1.AlertDashboard, error) {
	var affectedDashboards []monitoringv1alpha1.AlertDashboard

	// List all AlertDashboards in the same namespace
	dashboardList := &monitoringv1alpha1.AlertDashboardList{}
	if err := m.client.List(ctx, dashboardList, &client.ListOptions{
		Namespace: rule.Namespace,
	}); err != nil {
		return nil, fmt.Errorf("failed to list AlertDashboards: %w", err)
	}

	// Check each dashboard to see if it matches the rule
	for _, dashboard := range dashboardList.Items {
		if m.MatchesLabels(rule, &dashboard) {
			m.log.V(1).Info("Found affected dashboard",
				"dashboard", dashboard.Name,
				"rule", rule.Name)
			affectedDashboards = append(affectedDashboards, dashboard)
		}
	}

	return affectedDashboards, nil
}

func (m *defaultRuleManager) MatchesLabels(rule *monitoringv1.PrometheusRule, dashboard *monitoringv1alpha1.AlertDashboard) bool {
	// Check metadata labels (only check against rule's metadata labels)
	if !matchLabelSelector(rule, dashboard.Spec.MetadataLabelSelector, true) {
		return false
	}

	// Check rule labels (check against rule's metadata, group, and alert labels)
	return matchLabelSelector(rule, dashboard.Spec.RuleLabelSelector, false)
}

func checkMetadataLabels(rule *monitoringv1.PrometheusRule, selector *metav1.LabelSelector) bool {
	// Check matchLabels
	for k, v := range selector.MatchLabels {
		if resourceVal, ok := rule.Labels[k]; !ok || resourceVal != v {
			return false
		}
	}

	// Check matchExpressions
	for _, expr := range selector.MatchExpressions {
		if expr.Operator == metav1.LabelSelectorOpExists && !hasLabel(rule.Labels, expr.Key) {
			return false
		}
	}
	return true
}

func checkLabelsInGroups(rule *monitoringv1.PrometheusRule, key, value string) bool {
	for _, group := range rule.Spec.Groups {
		// Check group labels
		if groupVal, ok := group.Labels[key]; ok && groupVal == value {
			return true
		}

		// Check individual alert rule labels
		for _, alertRule := range group.Rules {
			// Skip rules with exclude label
			if _, excluded := alertRule.Labels[constants.LabelExcludeRule]; excluded {
				continue
			}
			if alertVal, ok := alertRule.Labels[key]; ok && alertVal == value {
				return true
			}
		}
	}
	return false
}

func hasLabel(labels map[string]string, key string) bool {
	_, ok := labels[key]
	return ok
}

func matchLabelSelector(rule *monitoringv1.PrometheusRule, selector *metav1.LabelSelector, metadataOnly bool) bool {
	if selector == nil {
		return true
	}

	if metadataOnly {
		return checkMetadataLabels(rule, selector)
	}

	// Check matchLabels
	for k, v := range selector.MatchLabels {
		if resourceVal, ok := rule.Labels[k]; ok && resourceVal == v {
			continue
		}
		if !checkLabelsInGroups(rule, k, v) {
			return false
		}
	}

	return true
}
