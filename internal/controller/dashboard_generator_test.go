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

	t.Run("GenerateDashboard with floor alert (lt operator)", func(t *testing.T) {
		dashboard := &monitoringv1alpha1.AlertDashboard{
			Spec: monitoringv1alpha1.AlertDashboardSpec{},
		}
		dashboard.Name = "floor-alert-dashboard"

		metrics := []model.GrafanaPanelQuery{
			{
				Name:      "CacheHitRatio",
				Query:     "cache_hit_ratio{app=\"myapp-persistence\"}",
				Threshold: 60.0,
				Operator:  "lt", // Floor alert: values < 60 are critical
			},
		}

		result, err := generator.GenerateDashboard(dashboard, metrics)
		require.NoError(t, err)
		assert.NotEmpty(t, result)

		// Verify the generated JSON contains the correct threshold configuration for floor alerts
		resultStr := string(result)
		assert.Contains(t, resultStr, "floor-alert-dashboard")
		assert.Contains(t, resultStr, "CacheHitRatio")
		assert.Contains(t, resultStr, "cache_hit_ratio{app=\\\"myapp-persistence\\\"}")

		// For floor alerts (lt operator), red should be at null (everything below threshold)
		// and green should be at the threshold value
		// Test the complete threshold structure in both places (thresholds and fieldConfig)
		expectedFloorThreshold := `"thresholds": {
        "mode": "absolute",
        "steps": [
          {
            "color": "red",
            "value": null
          },
          {
            "color": "green",
            "value": 60
          }
        ]
      }`
		expectedFloorFieldConfig := `"fieldConfig": {
        "defaults": {
          "custom": {
            "thresholdsStyle": {
              "mode": "line+area"
            }
          },
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "red",
                "value": null
              },
              {
                "color": "green",
                "value": 60
              }
            ]
          }
        }
      }`
		assert.Contains(t, resultStr, expectedFloorThreshold)
		assert.Contains(t, resultStr, expectedFloorFieldConfig)
	})

	t.Run("GenerateDashboard with ceiling alert (gt operator)", func(t *testing.T) {
		dashboard := &monitoringv1alpha1.AlertDashboard{
			Spec: monitoringv1alpha1.AlertDashboardSpec{},
		}
		dashboard.Name = "ceiling-alert-dashboard"

		metrics := []model.GrafanaPanelQuery{
			{
				Name:      "HighCPUUsage",
				Query:     "cpu_usage_percent",
				Threshold: 80.0,
				Operator:  "gt", // Ceiling alert: values > 80 are critical
			},
		}

		result, err := generator.GenerateDashboard(dashboard, metrics)
		require.NoError(t, err)
		assert.NotEmpty(t, result)

		// Verify the generated JSON contains the correct threshold configuration for ceiling alerts
		resultStr := string(result)
		assert.Contains(t, resultStr, "ceiling-alert-dashboard")
		assert.Contains(t, resultStr, "HighCPUUsage")
		assert.Contains(t, resultStr, "cpu_usage_percent")

		// For ceiling alerts (gt operator), green should be at null (everything below threshold)
		// and red should be at the threshold value
		// Test the complete threshold structure in both places (thresholds and fieldConfig)
		expectedCeilingThreshold := `"thresholds": {
        "mode": "absolute",
        "steps": [
          {
            "color": "green",
            "value": null
          },
          {
            "color": "red",
            "value": 80
          }
        ]
      }`
		expectedCeilingFieldConfig := `"fieldConfig": {
        "defaults": {
          "custom": {
            "thresholdsStyle": {
              "mode": "line+area"
            }
          },
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green",
                "value": null
              },
              {
                "color": "red",
                "value": 80
              }
            ]
          }
        }
      }`
		assert.Contains(t, resultStr, expectedCeilingThreshold)
		assert.Contains(t, resultStr, expectedCeilingFieldConfig)
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

	t.Run("should handle invalid metrics JSON marshaling", func(t *testing.T) {
		dashboard := &monitoringv1alpha1.AlertDashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name: "invalid-metrics-json",
			},
		}

		// Pass nil to force JSON marshaling error
		_, err := generator.GenerateDashboard(dashboard, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to evaluate template")
	})

	t.Run("should handle invalid JSON parsing after template evaluation", func(t *testing.T) {
		dashboard := &monitoringv1alpha1.AlertDashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name: "invalid-json-parse",
			},
			Spec: monitoringv1alpha1.AlertDashboardSpec{
				CustomJsonnetTemplate: `"invalid json"`, // This will produce invalid JSON
			},
		}

		metrics := []model.GrafanaPanelQuery{
			{
				Name:  "TestAlert",
				Query: "up == 0",
			},
		}
		_, err := generator.GenerateDashboard(dashboard, metrics)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse generated JSON")
	})

	t.Run("should handle invalid JSON formatting", func(t *testing.T) {
		dashboard := &monitoringv1alpha1.AlertDashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name: "invalid-json-format",
			},
			Spec: monitoringv1alpha1.AlertDashboardSpec{
				CustomJsonnetTemplate: `{
					title: std.extVar('title'),
					metrics: std.parseJson(std.extVar('metrics')),
					panels: [],
					// Add a circular reference to cause JSON marshaling error
					circular: self,
				}`,
			},
		}

		metrics := []model.GrafanaPanelQuery{
			{
				Name:  "TestAlert",
				Query: "up == 0",
			},
		}
		_, err := generator.GenerateDashboard(dashboard, metrics)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to evaluate template")
	})
}
