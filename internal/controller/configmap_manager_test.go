package controller

import (
	"context"
	"testing"

	"github.com/go-logr/logr/testr"
	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	"github.com/krutsko/alert2dash-operator/internal/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
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
		assert.Equal(t, "1", configMap.Labels[constants.LabelGrafanaDashboard])
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

	t.Run("should handle ConfigMap updates with unchanged content", func(t *testing.T) {
		client := fake.NewClientBuilder().WithScheme(scheme).Build()

		// Attempt to update with the same content
		manager := &defaultConfigMapManager{
			client: client,
			scheme: scheme,
			log:    ctrl.Log.WithName("test"),
		}

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

		content := []byte(`{"test": "data"}`)
		err := manager.CreateOrUpdateConfigMap(context.Background(), dashboard, content)
		require.NoError(t, err)
		// Verify no unnecessary updates were made
		configMap := &corev1.ConfigMap{}
		err = client.Get(context.Background(), types.NamespacedName{
			Name:      "grafana-dashboard-test-dashboard",
			Namespace: "default",
		}, configMap)
		require.NoError(t, err)
		require.Equal(t, configMap.ResourceVersion, "1")

		// Verify no unnecessary updates were made
		content2 := []byte(`{"test": "data"}`)
		_ = manager.CreateOrUpdateConfigMap(context.Background(), dashboard, content2)
		updatedConfigMap := &corev1.ConfigMap{}
		err = client.Get(context.Background(), types.NamespacedName{
			Name:      "grafana-dashboard-test-dashboard",
			Namespace: "default",
		}, updatedConfigMap)
		require.NoError(t, err)
		assert.Equal(t, configMap.ResourceVersion, updatedConfigMap.ResourceVersion)
	})

	t.Run("should update ConfigMap with missing labels", func(t *testing.T) {
		client := fake.NewClientBuilder().WithScheme(scheme).Build()

		// Create a ConfigMap without required labels
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "grafana-dashboard-test-dashboard",
				Namespace: "default",
			},
			Data: map[string]string{
				"test-dashboard.json": `{"test": "data"}`,
			},
		}
		require.NoError(t, client.Create(context.Background(), configMap))

		manager := &defaultConfigMapManager{
			client: client,
			scheme: scheme,
			log:    testLogger,
		}

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

		content := []byte(`{"test": "data"}`)
		err := manager.CreateOrUpdateConfigMap(context.Background(), dashboard, content)
		require.NoError(t, err)

		// Verify labels were added
		updatedConfigMap := &corev1.ConfigMap{}
		err = client.Get(context.Background(), types.NamespacedName{
			Name:      "grafana-dashboard-test-dashboard",
			Namespace: "default",
		}, updatedConfigMap)
		require.NoError(t, err)
		assert.Equal(t, "1", updatedConfigMap.Labels[constants.LabelGrafanaDashboard])
		assert.Equal(t, "test-dashboard", updatedConfigMap.Labels[constants.LabelDashboardName])
	})

	t.Run("should update ConfigMap with missing owner reference", func(t *testing.T) {
		client := fake.NewClientBuilder().WithScheme(scheme).Build()

		// Create a ConfigMap without owner reference
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "grafana-dashboard-test-dashboard",
				Namespace: "default",
				Labels: map[string]string{
					constants.LabelGrafanaDashboard: "1",
					constants.LabelDashboardName:    "test-dashboard",
				},
			},
			Data: map[string]string{
				"test-dashboard.json": `{"test": "data"}`,
			},
		}
		require.NoError(t, client.Create(context.Background(), configMap))

		manager := &defaultConfigMapManager{
			client: client,
			scheme: scheme,
			log:    testLogger,
		}

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

		content := []byte(`{"test": "data"}`)
		err := manager.CreateOrUpdateConfigMap(context.Background(), dashboard, content)
		require.NoError(t, err)

		// Verify owner reference was added
		updatedConfigMap := &corev1.ConfigMap{}
		err = client.Get(context.Background(), types.NamespacedName{
			Name:      "grafana-dashboard-test-dashboard",
			Namespace: "default",
		}, updatedConfigMap)
		require.NoError(t, err)
		require.Len(t, updatedConfigMap.OwnerReferences, 1)
		assert.Equal(t, dashboard.Name, updatedConfigMap.OwnerReferences[0].Name)
		assert.Equal(t, string(dashboard.UID), string(updatedConfigMap.OwnerReferences[0].UID))
	})

	t.Run("should handle ConfigMap with nil Data map", func(t *testing.T) {
		client := fake.NewClientBuilder().WithScheme(scheme).Build()

		// Create a ConfigMap with nil Data map
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "grafana-dashboard-test-dashboard",
				Namespace: "default",
				Labels: map[string]string{
					constants.LabelGrafanaDashboard: "1",
					constants.LabelDashboardName:    "test-dashboard",
				},
			},
		}
		require.NoError(t, client.Create(context.Background(), configMap))

		manager := &defaultConfigMapManager{
			client: client,
			scheme: scheme,
			log:    testLogger,
		}

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

		content := []byte(`{"test": "data"}`)
		err := manager.CreateOrUpdateConfigMap(context.Background(), dashboard, content)
		require.NoError(t, err)

		// Verify Data map was initialized and updated
		updatedConfigMap := &corev1.ConfigMap{}
		err = client.Get(context.Background(), types.NamespacedName{
			Name:      "grafana-dashboard-test-dashboard",
			Namespace: "default",
		}, updatedConfigMap)
		require.NoError(t, err)
		assert.NotNil(t, updatedConfigMap.Data)
		assert.Equal(t, string(content), updatedConfigMap.Data["test-dashboard.json"])
	})
}
