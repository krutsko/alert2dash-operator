package controller

import (
	"embed"
	"testing"

	"github.com/go-logr/logr/testr"
	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//go:embed templates/*
var testTemplates embed.FS

func TestDashboardGenerator(t *testing.T) {
	testLogger := testr.New(t)
	testLogger = testLogger.V(1) // Increase verbosity

	generator := &defaultDashboardGenerator{
		templates: testTemplates,
		log:       testLogger,
	}

	t.Run("GenerateDashboard with default template", func(t *testing.T) {
		dashboard := &monitoringv1alpha1.AlertDashboard{
			Spec: monitoringv1alpha1.AlertDashboardSpec{},
		}
		dashboard.Name = "test-dashboard"

		metricsJSON := []byte(`[
			{
				"name": "TestAlert",
				"query": "up == 0"
			}
		]`)

		result, err := generator.GenerateDashboard(dashboard, metricsJSON)
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

		metricsJSON := []byte(`{"test": "data"}`)

		result, err := generator.GenerateDashboard(dashboard, metricsJSON)
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

		_, err := generator.GenerateDashboard(dashboard, []byte(`{}`))
		assert.Error(t, err)
	})

	t.Run("GenerateDashboard with invalid metrics JSON", func(t *testing.T) {
		dashboard := &monitoringv1alpha1.AlertDashboard{
			Spec: monitoringv1alpha1.AlertDashboardSpec{},
		}
		dashboard.Name = "invalid-metrics"

		_, err := generator.GenerateDashboard(dashboard, []byte(`invalid json`))
		assert.Error(t, err)
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

		metrics := []byte(`[{"title": "Test Panel", "type": "graph"}]`)
		result, err := generator.GenerateDashboard(dashboard, metrics)
		require.NoError(t, err)
		assert.Contains(t, string(result), "Test Panel")
	})
}
