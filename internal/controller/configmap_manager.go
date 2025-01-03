package controller

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
		configMap.Labels["grafana_dashboard"] = "1"

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

func (m *defaultConfigMapManager) DeleteConfigMap(ctx context.Context, namespacedName types.NamespacedName) error {
	// Find all ConfigMaps that might contain our dashboard
	configMapList := &corev1.ConfigMapList{}
	if err := m.client.List(ctx, configMapList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"grafana_dashboard": "1",
		}),
	}); err != nil {
		return fmt.Errorf("failed to list ConfigMaps: %w", err)
	}

	var errs []string
	for _, cm := range configMapList.Items {
		if strings.HasSuffix(cm.Name, "-"+namespacedName.Name) {
			if err := m.client.Delete(ctx, &cm); err != nil {
				if !errors.IsNotFound(err) {
					errs = append(errs, fmt.Sprintf("failed to delete ConfigMap %s: %v", cm.Name, err))
				}
			} else {
				m.log.V(1).Info("Deleted dashboard ConfigMap",
					"namespace", cm.Namespace,
					"name", cm.Name)
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors occurred during ConfigMap deletion: %s", strings.Join(errs, "; "))
	}

	return nil
}
