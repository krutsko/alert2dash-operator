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
	log := m.log.WithValues("alertdashboard", dashboard.Name, "namespace", dashboard.Namespace)
	configMapName := fmt.Sprintf("%s-%s", dashboard.Spec.DashboardConfig.ConfigMapNamePrefix, dashboard.Name)
	dashboardKey := dashboard.Name + ".json"
	contentStr := string(content)

	// Create a configmap with the desired state
	desiredConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: dashboard.Namespace,
			Labels: map[string]string{
				constants.LabelGrafanaDashboard: "1",
				constants.LabelDashboardName:    dashboard.Name,
			},
		},
		Data: map[string]string{
			dashboardKey: contentStr,
		},
	}

	// Set owner reference
	if err := ctrl.SetControllerReference(dashboard, desiredConfigMap, m.scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Check if ConfigMap exists
	existingConfigMap := &corev1.ConfigMap{}
	err := m.client.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: dashboard.Namespace}, existingConfigMap)

	if client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to get ConfigMap %s: %w", configMapName, err)
	}

	// If ConfigMap doesn't exist, create a new one
	if err != nil {
		log.Info("Creating new ConfigMap", "name", configMapName)
		if err := m.client.Create(ctx, desiredConfigMap); err != nil {
			return fmt.Errorf("failed to create ConfigMap %s: %w", configMapName, err)
		}
		log.Info("ConfigMap created successfully", "name", configMapName)
		return nil
	}

	// ConfigMap exists, check if it needs updating
	if needsUpdate(existingConfigMap, dashboardKey, contentStr, dashboard) {
		// Update labels
		if existingConfigMap.Labels == nil {
			existingConfigMap.Labels = make(map[string]string)
		}
		existingConfigMap.Labels[constants.LabelGrafanaDashboard] = "1"
		existingConfigMap.Labels[constants.LabelDashboardName] = dashboard.Name

		// Update data
		if existingConfigMap.Data == nil {
			existingConfigMap.Data = make(map[string]string)
		}
		existingConfigMap.Data[dashboardKey] = contentStr

		// Ensure owner reference is set
		if err := ctrl.SetControllerReference(dashboard, existingConfigMap, m.scheme); err != nil {
			return fmt.Errorf("failed to set controller reference: %w", err)
		}

		if err := m.client.Update(ctx, existingConfigMap); err != nil {
			return fmt.Errorf("failed to update ConfigMap %s: %w", configMapName, err)
		}
		log.Info("ConfigMap updated successfully", "name", configMapName)
	} else {
		log.V(1).Info("ConfigMap is up to date, no changes needed", "name", configMapName)
	}

	return nil
}

// needsUpdate checks if the ConfigMap needs to be updated
func needsUpdate(existing *corev1.ConfigMap, dashboardKey, content string, dashboard *monitoringv1alpha1.AlertDashboard) bool {
	// Check labels
	if existing.Labels == nil ||
		existing.Labels[constants.LabelGrafanaDashboard] != "1" ||
		existing.Labels[constants.LabelDashboardName] != dashboard.Name {
		return true
	}

	// Check owner reference
	hasOwnerRef := false
	for _, ownerRef := range existing.OwnerReferences {
		if ownerRef.Name == dashboard.Name && string(ownerRef.UID) == string(dashboard.UID) {
			hasOwnerRef = true
			break
		}
	}
	if !hasOwnerRef {
		return true
	}

	// Check content
	existingContent, exists := existing.Data[dashboardKey]
	if !exists || existingContent != content {
		return true
	}

	return false
}
