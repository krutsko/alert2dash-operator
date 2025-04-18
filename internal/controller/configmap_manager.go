package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	"github.com/krutsko/alert2dash-operator/internal/constants"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// defaultConfigMapManager implements ConfigMapManager
type defaultConfigMapManager struct {
	client client.Client
	scheme *runtime.Scheme
	log    logr.Logger
}

func (m *defaultConfigMapManager) CreateOrUpdateConfigMap(ctx context.Context, dashboard *monitoringv1alpha1.AlertDashboard, content []byte) error {
	configMapName := fmt.Sprintf("%s-%s", dashboard.Spec.DashboardConfig.ConfigMapNamePrefix, dashboard.Name)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: dashboard.Namespace,
		},
	}

	op, err := ctrl.CreateOrUpdate(ctx, m.client, configMap, func() error {
		// Set or update labels
		if configMap.Labels == nil {
			configMap.Labels = make(map[string]string)
		}
		configMap.Labels[constants.LabelGrafanaDashboard] = "1"
		configMap.Labels[constants.LabelDashboardName] = dashboard.Name

		// Set or update data
		if configMap.Data == nil {
			configMap.Data = make(map[string]string)
		}
		configMap.Data[dashboard.Name+".json"] = string(content)

		// Set owner reference
		return ctrl.SetControllerReference(dashboard, configMap, m.scheme)
	})

	if err != nil {
		return fmt.Errorf("failed to create/update ConfigMap %s: %w", configMapName, err)
	}

	m.log.V(1).Info("ConfigMap operation completed",
		"name", configMapName,
		"operation", op,
		"namespace", dashboard.Namespace)

	return nil
}
