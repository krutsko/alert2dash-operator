package controller

import (
	"reflect"
	"testing"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestExtractBaseQuery(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		expected string
	}{
		{
			name:     "Simple greater than",
			expr:     "http_requests_total > 100",
			expected: "http_requests_total",
		},
		{
			name:     "Complex query with greater than or equal",
			expr:     "sum(rate(http_requests_total{code=~\"5..\"}[5m])) >= 10",
			expected: "sum(rate(http_requests_total{code=~\"5..\"}[5m]))",
		},
		{
			name:     "No comparison operator",
			expr:     "rate(http_requests_total[5m])",
			expected: "rate(http_requests_total[5m])",
		},
		{
			name:     "Multiple operators (should use first one)",
			expr:     "metric > 5 or metric < 1",
			expected: "metric",
		},
	}

	reconciler := &AlertDashboardReconciler{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.extractBaseQuery(tt.expr)
			if result != tt.expected {
				t.Errorf("extractBaseQuery() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMatchesLabels(t *testing.T) {
	tests := []struct {
		name     string
		rule     *monitoringv1.PrometheusRule
		selector *metav1.LabelSelector
		expected bool
	}{
		{
			name: "Match simple labels",
			rule: &monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test",
					},
				},
			},
			selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test",
				},
			},
			expected: true,
		},
		{
			name: "No match on simple labels",
			rule: &monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test",
					},
				},
			},
			selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "other",
				},
			},
			expected: false,
		},
		{
			name: "Match label in rule groups",
			rule: &monitoringv1.PrometheusRule{
				Spec: monitoringv1.PrometheusRuleSpec{
					Groups: []monitoringv1.RuleGroup{
						{
							Labels: map[string]string{
								"severity": "critical",
							},
							Rules: []monitoringv1.Rule{
								{
									Alert: "TestAlert",
									Expr:  intstr.FromString("test > 0"),
								},
							},
						},
					},
				},
			},
			selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"severity": "critical",
				},
			},
			expected: true,
		},
		{
			name: "Match labels in alert rules",
			rule: &monitoringv1.PrometheusRule{
				Spec: monitoringv1.PrometheusRuleSpec{
					Groups: []monitoringv1.RuleGroup{
						{
							Rules: []monitoringv1.Rule{
								{
									Alert: "TestAlert",
									Expr:  intstr.FromString("test > 0"),
									Labels: map[string]string{
										"team": "sre",
									},
								},
							},
						},
					},
				},
			},
			selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"team": "sre",
				},
			},
			expected: true,
		},
		{
			name: "Match with exists expression",
			rule: &monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"environment": "prod",
					},
				},
			},
			selector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "environment",
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			},
			expected: true,
		},
		{
			name: "No match with exists expression",
			rule: &monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test",
					},
				},
			},
			selector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "environment",
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			},
			expected: false,
		},
		{
			name: "Nil selector should match everything",
			rule: &monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test",
					},
				},
			},
			selector: nil,
			expected: true,
		},
	}

	reconciler := &AlertDashboardReconciler{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.matchesLabels(tt.rule, tt.selector)
			if result != tt.expected {
				t.Errorf("matchesLabels() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// Helper function to compare maps for deep equality
func mapsEqual(a, b map[string]string) bool {
	return reflect.DeepEqual(a, b)
}
