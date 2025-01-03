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

func (m *defaultRuleManager) GetPrometheusRules(ctx context.Context, namespace string, labelSelector labels.Selector) ([]monitoringv1.PrometheusRule, error) {
	ruleList := &monitoringv1.PrometheusRuleList{}
	listOpts := &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: labelSelector,
	}

	if err := m.client.List(ctx, ruleList, listOpts); err != nil {
		return nil, fmt.Errorf("failed to list PrometheusRules: %w", err)
	}

	var rules []monitoringv1.PrometheusRule
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
		if m.MatchesLabels(rule, dashboard.Spec.RuleSelector) {
			m.log.V(1).Info("Found affected dashboard",
				"dashboard", dashboard.Name,
				"rule", rule.Name)
			affectedDashboards = append(affectedDashboards, dashboard)
		}
	}

	return affectedDashboards, nil
}

func (m *defaultRuleManager) MatchesLabels(rule *monitoringv1.PrometheusRule, selector *metav1.LabelSelector) bool {
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
