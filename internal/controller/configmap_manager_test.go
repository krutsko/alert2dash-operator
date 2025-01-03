package controller

import (
	"context"
	"testing"

	"github.com/go-logr/logr/testr"
	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestConfigMapManager(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, monitoringv1alpha1.AddToScheme(scheme))

	// Create a test logger with debug level
	testLogger := testr.New(t)
	testLogger = testLogger.V(1) // Increase verbosity level

	t.Run("CreateOrUpdateConfigMap", func(t *testing.T) {
		// Create fake client
		client := fake.NewClientBuilder().WithScheme(scheme).Build()

		manager := &defaultConfigMapManager{
			client: client,
			scheme: scheme,
			log:    testLogger,
		}

		// Create test dashboard
		dashboard := &monitoringv1alpha1.AlertDashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dashboard",
				Namespace: "default",
			},
			Spec: monitoringv1alpha1.AlertDashboardSpec{
				DashboardConfig: monitoringv1alpha1.DashboardConfig{
					ConfigMapNamePrefix: "grafana-dashboard",
				},
			},
		}

		// Test content
		content := []byte(`{"dashboard": "test"}`)

		// Test creation
		err := manager.CreateOrUpdateConfigMap(context.Background(), dashboard, content)
		require.NoError(t, err)

		// Verify ConfigMap was created
		configMap := &corev1.ConfigMap{}
		err = client.Get(context.Background(), types.NamespacedName{
			Name:      "grafana-dashboard-test-dashboard",
			Namespace: "default",
		}, configMap)

		require.NoError(t, err)
		assert.Equal(t, "1", configMap.Labels["grafana_dashboard"])
		assert.Equal(t, string(content), configMap.Data["test-dashboard.json"])

		// Test update
		newContent := []byte(`{"dashboard": "updated"}`)
		err = manager.CreateOrUpdateConfigMap(context.Background(), dashboard, newContent)
		require.NoError(t, err)

		// Verify update
		err = client.Get(context.Background(), types.NamespacedName{
			Name:      "grafana-dashboard-test-dashboard",
			Namespace: "default",
		}, configMap)

		require.NoError(t, err)
		assert.Equal(t, string(newContent), configMap.Data["test-dashboard.json"])
	})

	t.Run("DeleteConfigMap", func(t *testing.T) {
		// Create fake client with existing ConfigMap
		existingConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "grafana-dashboard-test-dashboard",
				Namespace: "default",
				Labels: map[string]string{
					"grafana_dashboard": "1",
				},
			},
		}

		client := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(existingConfigMap).
			Build()

		manager := &defaultConfigMapManager{
			client: client,
			scheme: scheme,
			log:    testLogger,
		}

		// Test deletion
		err := manager.DeleteConfigMap(context.Background(), types.NamespacedName{
			Name:      "test-dashboard",
			Namespace: "default",
		})
		require.NoError(t, err)

		// Verify ConfigMap was deleted
		configMap := &corev1.ConfigMap{}
		err = client.Get(context.Background(), types.NamespacedName{
			Name:      "grafana-dashboard-test-dashboard",
			Namespace: "default",
		}, configMap)

		assert.Error(t, err) // Should error because ConfigMap is deleted
	})

	t.Run("DeleteConfigMap_NonExistent", func(t *testing.T) {
		// Create fake client with no existing ConfigMaps
		client := fake.NewClientBuilder().WithScheme(scheme).Build()

		manager := &defaultConfigMapManager{
			client: client,
			scheme: scheme,
			log:    testLogger,
		}

		// Test deletion of non-existent ConfigMap
		err := manager.DeleteConfigMap(context.Background(), types.NamespacedName{
			Name:      "non-existent",
			Namespace: "default",
		})

		require.NoError(t, err) // Should not error when ConfigMap doesn't exist
	})

	t.Run("CreateConfigMap_WithOwnerReference", func(t *testing.T) {
		client := fake.NewClientBuilder().WithScheme(scheme).Build()

		manager := &defaultConfigMapManager{
			client: client,
			scheme: scheme,
			log:    testLogger,
		}

		// Create test dashboard with specific UID
		dashboard := &monitoringv1alpha1.AlertDashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dashboard",
				Namespace: "default",
				UID:       "test-uid",
			},
			Spec: monitoringv1alpha1.AlertDashboardSpec{
				DashboardConfig: monitoringv1alpha1.DashboardConfig{
					ConfigMapNamePrefix: "grafana-dashboard",
				},
			},
		}

		content := []byte(`{"dashboard": "test"}`)

		// Create ConfigMap
		err := manager.CreateOrUpdateConfigMap(context.Background(), dashboard, content)
		require.NoError(t, err)

		// Verify owner reference
		configMap := &corev1.ConfigMap{}
		err = client.Get(context.Background(), types.NamespacedName{
			Name:      "grafana-dashboard-test-dashboard",
			Namespace: "default",
		}, configMap)

		require.NoError(t, err)
		require.Len(t, configMap.OwnerReferences, 1)
		assert.Equal(t, dashboard.Name, configMap.OwnerReferences[0].Name)
		assert.Equal(t, string(dashboard.UID), string(configMap.OwnerReferences[0].UID))
	})
}
