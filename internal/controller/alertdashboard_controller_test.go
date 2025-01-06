/*
Copyright (c) 2024

Licensed under MIT License. See LICENSE file in the root directory of this repository.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	kuberr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("AlertDashboard Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()
		namespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			// Ensure no existing resources
			By("ensuring no existing resources")
			Eventually(func() error {
				alertDashboard := &monitoringv1alpha1.AlertDashboard{}
				err := k8sClient.Get(ctx, namespacedName, alertDashboard)
				if err == nil || !errors.IsNotFound(err) {
					return fmt.Errorf("AlertDashboard still exists or error: %v", err)
				}
				return nil
			}, "30s", "1s").Should(Succeed())

			duration := monitoringv1.Duration("5m")
			By("creating test PrometheusRule with matching labels")
			prometheusRule := &monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rule",
					Namespace: "default",
					Labels: map[string]string{
						"app":                "test-app",
						"generate-dashboard": "true",
					},
				},
				Spec: monitoringv1.PrometheusRuleSpec{
					Groups: []monitoringv1.RuleGroup{
						{
							Name: "test.rules",
							Rules: []monitoringv1.Rule{
								{
									Alert: "HighErrorRate",
									Expr:  intstr.FromString("sum(rate(http_requests_total{code=~\"5..\"}[5m])) / sum(rate(http_requests_total[5m])) > 0.1"),
									For:   &duration,
									Labels: map[string]string{
										"severity": "warning",
										"team":     "dreamteam",
										"tier":     "backend",
									},
									Annotations: map[string]string{
										"description": "Error rate is high",
										"summary":     "High HTTP error rate detected",
									},
								},
								{
									Alert: "HighLatency",
									Expr:  intstr.FromString("histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m])) > 1"),
									For:   &duration,
									Labels: map[string]string{
										"severity": "warning",
										"team":     "dreamteam",
										"tier":     "backend",
									},
									Annotations: map[string]string{
										"description": "95th percentile latency is high",
										"summary":     "High latency detected",
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, prometheusRule)).To(Succeed())

			By("creating the custom resource for the Kind AlertDashboard")
			alertDashboard := &monitoringv1alpha1.AlertDashboard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: monitoringv1alpha1.AlertDashboardSpec{
					RuleSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app":                "test-app",
							"generate-dashboard": "true",
						},
					},
					DashboardConfig: monitoringv1alpha1.DashboardConfig{
						Title:               "Test Dashboard",
						ConfigMapNamePrefix: "grafana-dashboard",
					},
				},
			}
			Expect(k8sClient.Create(ctx, alertDashboard)).To(Succeed())
		})

		It("should create a ConfigMap with dashboard panels for matching rules", func() {
			By("triggering a reconciliation")
			reconciler := NewAlertDashboardReconciler(
				k8sClient,
				k8sClient.Scheme(),
				ctrl.Log.WithName("controllers").WithName("test"),
			)

			// First reconciliation - should add finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait for finalizer to be added
			Eventually(func() bool {
				dashboard := &monitoringv1alpha1.AlertDashboard{}
				err := k8sClient.Get(ctx, namespacedName, dashboard)
				if err != nil {
					return false
				}
				return hasDashboardFinalizer(dashboard.Finalizers)
			}, "10s", "1s").Should(BeTrue())

			// Second reconciliation - should process dashboard
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("checking if the ConfigMap was created")
			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "grafana-dashboard-" + resourceName,
					Namespace: "default",
				}, configMap)
				if err != nil {
					return err
				}
				if configMap.Labels["grafana_dashboard"] != "1" {
					return fmt.Errorf("expected grafana_dashboard label to be 1")
				}
				return nil
			}, "10s", "1s").Should(Succeed())

			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "grafana-dashboard-" + resourceName,
				Namespace: "default",
			}, configMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(configMap.Labels["grafana_dashboard"]).To(Equal("1"))

			By("verifying the dashboard JSON structure and content")
			dashboardJson := configMap.Data[resourceName+".json"]

			Expect(dashboardJson).To(ContainSubstring(`"expr": "sum(rate(http_requests_total{code=~\"5..\"}[5m])) / sum(rate(http_requests_total[5m]))"`))
			Expect(dashboardJson).To(ContainSubstring(`"expr": "histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))"`))
			Expect(dashboardJson).To(ContainSubstring(`"title": "HighErrorRate"`))
			Expect(dashboardJson).To(ContainSubstring(`"title": "HighLatency"`))
		})

		It("should handle dashboard deletion correctly", func() {
			By("waiting for the dashboard to be fully created")
			reconciler := NewAlertDashboardReconciler(
				k8sClient,
				k8sClient.Scheme(),
				ctrl.Log.WithName("controllers").WithName("test"),
			)

			// First reconciliation - should add finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait for finalizer to be added
			Eventually(func() bool {
				dashboard := &monitoringv1alpha1.AlertDashboard{}
				err := k8sClient.Get(ctx, namespacedName, dashboard)
				if err != nil {
					return false
				}
				return hasDashboardFinalizer(dashboard.Finalizers)
			}, "10s", "1s").Should(BeTrue())

			// Second reconciliation - should process dashboard
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("deleting the AlertDashboard resource")
			alertDashboard := &monitoringv1alpha1.AlertDashboard{}
			err = k8sClient.Get(ctx, namespacedName, alertDashboard)
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Delete(ctx, alertDashboard)).To(Succeed())

			By("triggering a reconciliation after deletion")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the ConfigMap was deleted")
			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				err = k8sClient.Get(ctx, types.NamespacedName{
					Name:      "grafana-dashboard-" + resourceName,
					Namespace: "default",
				}, configMap)
				return err
			}, "10s", "1s").Should(Satisfy(kuberr.IsNotFound))
		})

		It("should not create ConfigMap when no PrometheusRules match", func() {
			const resourceNameWithoutRules = "test-resource-no-rules"
			By("creating the custom resource without matching PrometheusRules")
			alertDashboard := &monitoringv1alpha1.AlertDashboard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceNameWithoutRules,
					Namespace: "default",
				},
				Spec: monitoringv1alpha1.AlertDashboardSpec{
					RuleSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"non-existent": "label",
						},
					},
					DashboardConfig: monitoringv1alpha1.DashboardConfig{
						Title:               "Empty Dashboard",
						ConfigMapNamePrefix: "grafana-dashboard",
					},
				},
			}
			Expect(k8sClient.Create(ctx, alertDashboard)).To(Succeed())

			By("triggering a reconciliation")
			reconciler := NewAlertDashboardReconciler(
				k8sClient,
				k8sClient.Scheme(),
				ctrl.Log.WithName("controllers").WithName("test"),
			)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying no ConfigMap was created")
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "grafana-dashboard-" + resourceNameWithoutRules,
				Namespace: "default",
			}, configMap)
			Expect(err).To(HaveOccurred())
			Expect(kuberr.IsNotFound(err)).To(BeTrue())
		})

		AfterEach(func() {
			By("cleaning up the PrometheusRule")
			prometheusRule := &monitoringv1.PrometheusRule{}
			err := handleResourceDeletion(ctx, k8sClient, prometheusRule, types.NamespacedName{
				Name:      "test-rule",
				Namespace: "default",
			})
			Expect(err).NotTo(HaveOccurred())

			By("cleaning up the AlertDashboard resource")
			alertDashboard := &monitoringv1alpha1.AlertDashboard{}
			err = handleResourceDeletion(ctx, k8sClient, alertDashboard, namespacedName)
			Expect(err).NotTo(HaveOccurred())

			By("cleaning up any remaining ConfigMaps")
			configMap := &corev1.ConfigMap{}
			err = handleResourceDeletion(ctx, k8sClient, configMap, types.NamespacedName{
				Name:      "grafana-dashboard-" + resourceName,
				Namespace: "default",
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

func removeFinalizers(ctx context.Context, c client.Client, obj client.Object) error {
	// Create a patch from the original object
	patch := client.MergeFrom(obj.DeepCopyObject().(client.Object))
	// Remove finalizers
	obj.SetFinalizers(nil)
	// Apply the patch
	return c.Patch(ctx, obj, patch)
}

// waitForDeletion waits until the object is deleted
func waitForDeletion(ctx context.Context, c client.Client, namespacedName types.NamespacedName, obj client.Object) error {
	return wait.PollUntilContextTimeout(ctx, time.Second, time.Second*10, true, func(ctx context.Context) (bool, error) {
		err := c.Get(ctx, namespacedName, obj)
		if err != nil && kuberr.IsNotFound(err) {
			return true, nil
		}
		return false, nil
	})
}

// handleResourceDeletion handles the complete deletion process including finalizer removal
func handleResourceDeletion(ctx context.Context, c client.Client, obj client.Object, namespacedName types.NamespacedName) error {
	// First check if resource exists
	err := c.Get(ctx, namespacedName, obj)
	if err != nil {
		if kuberr.IsNotFound(err) {
			return nil
		}
		return err
	}

	// Remove finalizers first
	if err := removeFinalizers(ctx, c, obj); err != nil {
		return fmt.Errorf("failed to remove finalizers: %w", err)
	}

	// Delete the resource
	if err := c.Delete(ctx, obj); err != nil && !kuberr.IsNotFound(err) {
		return fmt.Errorf("failed to delete resource: %w", err)
	}

	// Wait for actual deletion
	return waitForDeletion(ctx, c, namespacedName, obj.DeepCopyObject().(client.Object))
}
