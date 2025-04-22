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

	// Check if ConfigMap exists
	existingConfigMap := &corev1.ConfigMap{}
	err := m.client.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: dashboard.Namespace}, existingConfigMap)

	if err != nil {
		// ConfigMap doesn't exist, create a new one
		m.log.V(1).Info("ConfigMap not found, creating new one",
			"name", configMapName,
			"namespace", dashboard.Namespace)

		newConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: dashboard.Namespace,
				Labels: map[string]string{
					constants.LabelGrafanaDashboard: "1",
					constants.LabelDashboardName:    dashboard.Name,
				},
			},
			Data: map[string]string{
				dashboard.Name + ".json": string(content),
			},
		}

		// Set owner reference
		if err := ctrl.SetControllerReference(dashboard, newConfigMap, m.scheme); err != nil {
			return fmt.Errorf("failed to set controller reference: %w", err)
		}

		if err := m.client.Create(ctx, newConfigMap); err != nil {
			return fmt.Errorf("failed to create ConfigMap %s: %w", configMapName, err)
		}

		m.log.V(1).Info("ConfigMap created successfully",
			"name", configMapName,
			"namespace", dashboard.Namespace)
		return nil
	}

	// ConfigMap exists, check if content needs updating
	dashboardKey := dashboard.Name + ".json"
	currentContent, exists := existingConfigMap.Data[dashboardKey]

	// Check if update is needed
	needsUpdate := false

	// Check labels
	if existingConfigMap.Labels == nil ||
		existingConfigMap.Labels[constants.LabelGrafanaDashboard] != "1" ||
		existingConfigMap.Labels[constants.LabelDashboardName] != dashboard.Name {
		needsUpdate = true
	}

	// Check content
	if !exists || currentContent != string(content) {
		needsUpdate = true
	}

	if needsUpdate {
		// Update the ConfigMap
		if existingConfigMap.Labels == nil {
			existingConfigMap.Labels = make(map[string]string)
		}
		existingConfigMap.Labels[constants.LabelGrafanaDashboard] = "1"
		existingConfigMap.Labels[constants.LabelDashboardName] = dashboard.Name

		if existingConfigMap.Data == nil {
			existingConfigMap.Data = make(map[string]string)
		}
		existingConfigMap.Data[dashboardKey] = string(content)

		// Ensure owner reference is set
		if err := ctrl.SetControllerReference(dashboard, existingConfigMap, m.scheme); err != nil {
			return fmt.Errorf("failed to set controller reference: %w", err)
		}

		if err := m.client.Update(ctx, existingConfigMap); err != nil {
			return fmt.Errorf("failed to update ConfigMap %s: %w", configMapName, err)
		}

		m.log.V(1).Info("ConfigMap updated successfully",
			"name", configMapName,
			"namespace", dashboard.Namespace)
	} else {
		m.log.V(1).Info("ConfigMap is up to date, no changes needed",
			"name", configMapName,
			"namespace", dashboard.Namespace)
	}

	return nil
}
