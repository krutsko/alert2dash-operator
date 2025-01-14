package controller

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestRuleManager(t *testing.T) {
	// Setup
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, monitoringv1.AddToScheme(scheme))
	require.NoError(t, monitoringv1alpha1.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	manager := &defaultRuleManager{
		client: fakeClient,
		log:    logr.Discard(),
	}

	// Base rule with nested labels at different levels
	baseRule := &monitoringv1.PrometheusRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rule",
			Namespace: "default",
			Labels: map[string]string{
				"app":       "test",
				"team":      "sre",
				"component": "monitoring",
			},
		},
		Spec: monitoringv1.PrometheusRuleSpec{
			Groups: []monitoringv1.RuleGroup{
				{
					Name: "test-group-1",
					Labels: map[string]string{
						"severity": "critical",
						"tier":     "backend",
					},
					Rules: []monitoringv1.Rule{
						{
							Alert: "TestAlert1",
							Expr:  intstr.FromString("up == 0"),
							Labels: map[string]string{
								"alert_type": "availability",
								"service":    "api",
							},
						},
					},
				},
				{
					Name: "test-group-2",
					Labels: map[string]string{
						"severity": "warning",
						"tier":     "frontend",
					},
					Rules: []monitoringv1.Rule{
						{
							Alert: "TestAlert2",
							Expr:  intstr.FromString("error_rate > 0.1"),
							Labels: map[string]string{
								"alert_type": "performance",
								"service":    "web",
							},
						},
					},
				},
			},
		},
	}

	// Create test AlertDashboard
	dashboard := &monitoringv1alpha1.AlertDashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dashboard",
			Namespace: "default",
		},
		Spec: monitoringv1alpha1.AlertDashboardSpec{
			MetadataLabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test",
				},
			},
		},
	}

	// Create resources in fake client
	require.NoError(t, fakeClient.Create(ctx, baseRule))
	require.NoError(t, fakeClient.Create(ctx, dashboard))

	t.Run("GetPrometheusRules", func(t *testing.T) {
		t.Log("Testing GetPrometheusRules with nil selector")
		// Test with nil selector
		dashboardWithNilSelector := dashboard.DeepCopy()
		dashboardWithNilSelector.Spec.MetadataLabelSelector = nil
		rules, err := manager.GetPrometheusRules(ctx, dashboardWithNilSelector)
		require.NoError(t, err)
		assert.Len(t, rules, 1, "Should return all rules when selector is nil")
		assert.Equal(t, "test-rule", rules[0].Name)

		t.Log("Testing GetPrometheusRules with non-existent selector")
		// Test with non-existent label selector
		dashboardWithNonExistentSelector := dashboard.DeepCopy()
		dashboardWithNonExistentSelector.Spec.MetadataLabelSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{"non": "existent"},
		}
		rules, err = manager.GetPrometheusRules(ctx, dashboardWithNonExistentSelector)
		require.NoError(t, err)
		assert.Empty(t, rules)

		t.Log("Testing GetPrometheusRules with valid selector")
		// Test with valid selector
		dashboardWithValidSelector := dashboard.DeepCopy()
		dashboardWithValidSelector.Spec.MetadataLabelSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{"app": "test"},
		}
		rules, err = manager.GetPrometheusRules(ctx, dashboardWithValidSelector)
		require.NoError(t, err)
		assert.Len(t, rules, 1)
		assert.Equal(t, "test-rule", rules[0].Name)
	})

	t.Run("FindAffectedDashboards", func(t *testing.T) {
		t.Log("Testing FindAffectedDashboards with rule in different namespace")
		// Test with rule in different namespace
		ruleInDiffNS := baseRule.DeepCopy()
		ruleInDiffNS.Namespace = "other"
		dashboards, err := manager.FindAffectedDashboards(ctx, ruleInDiffNS)
		require.NoError(t, err)
		assert.Empty(t, dashboards)

		t.Log("Testing FindAffectedDashboards with non-matching labels")
		// Test with rule having no matching labels
		ruleNoMatch := baseRule.DeepCopy()
		ruleNoMatch.Labels = map[string]string{"non": "matching"}
		dashboards, err = manager.FindAffectedDashboards(ctx, ruleNoMatch)
		require.NoError(t, err)
		assert.Empty(t, dashboards)

		t.Log("Testing FindAffectedDashboards with matching rule")
		// Test with matching rule
		dashboards, err = manager.FindAffectedDashboards(ctx, baseRule)
		require.NoError(t, err)
		assert.Len(t, dashboards, 1)
		assert.Equal(t, "test-dashboard", dashboards[0].Name)
	})

	tests := []struct {
		name             string
		rule             *monitoringv1.PrometheusRule
		metadataSelector *metav1.LabelSelector
		ruleSelector     *metav1.LabelSelector
		want             bool
		description      string
	}{
		{
			name:             "both selectors nil",
			rule:             baseRule,
			metadataSelector: nil,
			ruleSelector:     nil,
			want:             true,
			description:      "Should match when both selectors are nil",
		},
		{
			name: "matching metadata labels only",
			rule: baseRule,
			metadataSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":  "test",
					"team": "sre",
				},
			},
			ruleSelector: nil,
			want:         true,
			description:  "Should match when metadata labels match and rule selector is nil",
		},
		{
			name: "non-matching metadata labels",
			rule: baseRule,
			metadataSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "nonexistent",
				},
			},
			ruleSelector: nil,
			want:         false,
			description:  "Should not match when metadata labels don't match",
		},
		{
			name:             "matching rule group labels",
			rule:             baseRule,
			metadataSelector: nil,
			ruleSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"severity": "critical",
					"tier":     "backend",
				},
			},
			want:        true,
			description: "Should match when rule group labels match",
		},
		{
			name:             "matching alert labels",
			rule:             baseRule,
			metadataSelector: nil,
			ruleSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"alert_type": "availability",
					"service":    "api",
				},
			},
			want:        true,
			description: "Should match when alert labels match",
		},
		{
			name: "mixed level label matching",
			rule: baseRule,
			metadataSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test",
				},
			},
			ruleSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"severity":   "warning",
					"alert_type": "performance",
				},
			},
			want:        true,
			description: "Should match when labels from different levels match",
		},
		{
			name: "exists operator on metadata",
			rule: baseRule,
			metadataSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "app",
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			},
			ruleSelector: nil,
			want:         true,
			description:  "Should match when exists operator matches metadata label",
		},
		{
			name:             "exists operator on rule labels",
			rule:             baseRule,
			metadataSelector: nil,
			ruleSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "alert_type",
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			},
			want:        true,
			description: "Should match when exists operator matches rule label",
		},
		{
			name: "exists operator non-matching",
			rule: baseRule,
			metadataSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "nonexistent",
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			},
			ruleSelector: nil,
			want:         false,
			description:  "Should not match when exists operator doesn't find label",
		},
		{
			name: "complex mixed matching",
			rule: baseRule,
			metadataSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test",
				},
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "team",
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			},
			ruleSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"severity": "warning",
				},
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "service",
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			},
			want:        true,
			description: "Should match complex combination of label requirements",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Running test case: %s", tt.name)
			dashboard := &monitoringv1alpha1.AlertDashboard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dashboard",
					Namespace: "default",
				},
				Spec: monitoringv1alpha1.AlertDashboardSpec{
					MetadataLabelSelector: tt.metadataSelector,
					RuleLabelSelector:     tt.ruleSelector,
				},
			}

			got := manager.MatchesLabels(tt.rule, dashboard)
			assert.Equal(t, tt.want, got, tt.description)
		})
	}
}
