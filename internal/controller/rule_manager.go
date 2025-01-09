package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
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
	listOpts := &client.ListOptions{
		Namespace:     dashboard.Namespace,
		LabelSelector: labels.SelectorFromSet(dashboard.Spec.MetadataLabelSelector.MatchLabels),
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
	if !matchLabelSelector(rule, dashboard.Spec.RuleLabelSelector, false) {
		return false
	}

	return true
}

// matchLabelSelector checks if the rule matches the given selector
// metadataOnly: if true, only checks resource metadata labels; if false, checks rule and group labels too
func matchLabelSelector(rule *monitoringv1.PrometheusRule, selector *metav1.LabelSelector, metadataOnly bool) bool {
	if selector == nil {
		return true
	}

	// For metadata-only checks, only use rule.Labels
	if metadataOnly {
		// Check matchLabels
		for k, v := range selector.MatchLabels {
			if resourceVal, ok := rule.Labels[k]; !ok || resourceVal != v {
				return false
			}
		}

		// Check matchExpressions
		for _, expr := range selector.MatchExpressions {
			switch expr.Operator {
			case metav1.LabelSelectorOpExists:
				if _, ok := rule.Labels[expr.Key]; !ok {
					return false
				}
			}
			// Add other operators as needed
		}
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

		// If we're only checking metadata labels and we didn't find a match, fail
		if metadataOnly {
			return false
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

			// If not found in metadata and we're only checking metadata, fail
			if !exists && metadataOnly {
				return false
			}

			// Check labels in rule groups and alert rules if not metadata only
			if !exists && !metadataOnly {
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
		// Add other operators as needed
	}

	return true
}
