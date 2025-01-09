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

	// Create a new scheme and register the types
	scheme := runtime.NewScheme()
	require.NoError(t, monitoringv1.AddToScheme(scheme))
	require.NoError(t, monitoringv1alpha1.AddToScheme(scheme))

	// Create fake client with the scheme
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	manager := &defaultRuleManager{
		client: fakeClient,
		log:    logr.Discard(),
	}

	// Create test PrometheusRule
	rule := &monitoringv1.PrometheusRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rule",
			Namespace: "default",
			Labels: map[string]string{
				"app": "test",
			},
		},
		Spec: monitoringv1.PrometheusRuleSpec{
			Groups: []monitoringv1.RuleGroup{
				{
					Name: "test-group",
					Rules: []monitoringv1.Rule{
						{
							Alert: "TestAlert",
							Expr:  intstr.FromString("up == 0"),
							Labels: map[string]string{
								"severity": "critical",
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
	require.NoError(t, fakeClient.Create(ctx, rule))
	require.NoError(t, fakeClient.Create(ctx, dashboard))

	t.Run("GetPrometheusRules", func(t *testing.T) {
		// Test with non-existent label selector
		dashboardWithNonExistentSelector := dashboard.DeepCopy()
		dashboardWithNonExistentSelector.Spec.MetadataLabelSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{"non": "existent"},
		}
		rules, err := manager.GetPrometheusRules(ctx, dashboardWithNonExistentSelector)
		require.NoError(t, err)
		assert.Empty(t, rules)

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
		// Test with rule in different namespace
		ruleInDiffNS := rule.DeepCopy()
		ruleInDiffNS.Namespace = "other"
		dashboards, err := manager.FindAffectedDashboards(ctx, ruleInDiffNS)
		require.NoError(t, err)
		assert.Empty(t, dashboards)

		// Test with rule having no matching labels
		ruleNoMatch := rule.DeepCopy()
		ruleNoMatch.Labels = map[string]string{"non": "matching"}
		dashboards, err = manager.FindAffectedDashboards(ctx, ruleNoMatch)
		require.NoError(t, err)
		assert.Empty(t, dashboards)

		// Test with matching rule
		dashboards, err = manager.FindAffectedDashboards(ctx, rule)
		require.NoError(t, err)
		assert.Len(t, dashboards, 1)
		assert.Equal(t, "test-dashboard", dashboards[0].Name)
	})

	t.Run("MatchesLabels", func(t *testing.T) {
		testCases := []struct {
			name     string
			selector *metav1.LabelSelector
			want     bool
		}{
			{
				name: "matching metadata labels",
				selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
				want: true,
			},
			{
				name: "matching alert labels",
				selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"severity": "critical"},
				},
				want: true,
			},
			{
				name: "non-matching labels",
				selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "nonexistent"},
				},
				want: false,
			},
			{
				name:     "nil selector",
				selector: nil,
				want:     true,
			},
			{
				name: "exists operator",
				selector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "app",
							Operator: metav1.LabelSelectorOpExists,
						},
					},
				},
				want: true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				got := manager.MatchesLabels(rule, tc.selector)
				assert.Equal(t, tc.want, got)
			})
		}
	})
}
