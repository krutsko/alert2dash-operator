package controller

import (
	"testing"

	"github.com/go-logr/logr/testr"
	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	templates "github.com/krutsko/alert2dash-operator/internal/embedfs"
	"github.com/krutsko/alert2dash-operator/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDashboardGenerator(t *testing.T) {
	testLogger := testr.New(t)
	testLogger = testLogger.V(1) // Increase verbosity

	generator := &defaultDashboardGenerator{
		templates: templates.GrafonnetTemplates,
		log:       testLogger,
	}

	t.Run("GenerateDashboard with default template", func(t *testing.T) {
		dashboard := &monitoringv1alpha1.AlertDashboard{
			Spec: monitoringv1alpha1.AlertDashboardSpec{},
		}
		dashboard.Name = "test-dashboard"

		metrics := []model.GrafanaPanelQuery{
			{
				Name:  "TestAlert",
				Query: "up == 0",
			},
		}

		result, err := generator.GenerateDashboard(dashboard, metrics)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, string(result), "test-dashboard")
	})

	t.Run("GenerateDashboard with custom template", func(t *testing.T) {
		dashboard := &monitoringv1alpha1.AlertDashboard{
			Spec: monitoringv1alpha1.AlertDashboardSpec{
				CustomJsonnetTemplate: `{
					title: std.extVar('title'),
					metrics: std.parseJson(std.extVar('metrics')),
					panels: [],
				}`,
			},
		}
		dashboard.Name = "custom-dashboard"

		metrics := []model.GrafanaPanelQuery{
			{
				Name:  "TestAlert",
				Query: "up == 0",
			},
		}

		result, err := generator.GenerateDashboard(dashboard, metrics)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, string(result), "custom-dashboard")
	})

	t.Run("GenerateDashboard with invalid template", func(t *testing.T) {
		dashboard := &monitoringv1alpha1.AlertDashboard{
			Spec: monitoringv1alpha1.AlertDashboardSpec{
				CustomJsonnetTemplate: `invalid jsonnet`,
			},
		}
		dashboard.Name = "invalid-dashboard"

		_, err := generator.GenerateDashboard(dashboard, []model.GrafanaPanelQuery{})
		assert.Error(t, err)
	})

	t.Run("GenerateDashboard with empty metrics JSON", func(t *testing.T) {
		dashboard := &monitoringv1alpha1.AlertDashboard{
			Spec: monitoringv1alpha1.AlertDashboardSpec{},
		}
		dashboard.Name = "invalid-metrics"

		generatedDashboard, err := generator.GenerateDashboard(dashboard, []model.GrafanaPanelQuery{})
		require.NoError(t, err)
		assert.NotEmpty(t, generatedDashboard)

		// Verify the dashboard contains empty panels
		dashboardStr := string(generatedDashboard)
		assert.Contains(t, dashboardStr, "invalid-metrics")
		assert.Contains(t, dashboardStr, `"panels": []`)
	})

	t.Run("should handle custom Jsonnet templates", func(t *testing.T) {
		dashboard := &monitoringv1alpha1.AlertDashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "custom-template",
				Namespace: "default",
			},
			Spec: monitoringv1alpha1.AlertDashboardSpec{
				CustomJsonnetTemplate: `{
						title: std.extVar('title'),
						panels: std.parseJson(std.extVar('metrics')),
					}`,
			},
		}

		metrics := []model.GrafanaPanelQuery{
			{
				Name:  "TestAlert",
				Query: "up == 0",
			},
		}
		result, err := generator.GenerateDashboard(dashboard, metrics)
		require.NoError(t, err)
		assert.Contains(t, string(result), "TestAlert")
	})
}
