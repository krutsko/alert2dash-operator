/*
Copyright (c) 2024

Licensed under MIT License. See LICENSE file in the root directory of this repository.
*/

package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/prometheus/prometheus/promql/parser"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
				return containsString(dashboard.Finalizers, dashboardFinalizer)
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
				return containsString(dashboard.Finalizers, dashboardFinalizer)
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

		It("should handle dashboard deletion correctly", func() {
			By("deleting the AlertDashboard resource")
			alertDashboard := &monitoringv1alpha1.AlertDashboard{}
			err := handleResourceDeletion(ctx, k8sClient, alertDashboard, namespacedName)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the ConfigMap was deleted")
			configMap := &corev1.ConfigMap{}
			configMapName := types.NamespacedName{
				Name:      "grafana-dashboard-" + resourceName,
				Namespace: "default",
			}

			Eventually(func() error {
				return k8sClient.Get(ctx, configMapName, configMap)
			}, "10s", "1s").Should(Satisfy(kuberr.IsNotFound))
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

func TestExtractBaseQuery(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		expected []string
	}{
		{
			name:     "greater than operator",
			expr:     "sum(rate(http_requests_total[5m])) > 100",
			expected: []string{"sum(rate(http_requests_total[5m]))"},
		},
		{
			name:     "less than operator",
			expr:     "node_memory_MemAvailable_bytes < 1000000",
			expected: []string{"node_memory_MemAvailable_bytes"},
		},
		{
			name:     "greater than or equal operator",
			expr:     "rate(errors_total[5m]) >= 0.5",
			expected: []string{"rate(errors_total[5m])"},
		},
		{
			name:     "equals operator",
			expr:     `up{job="kubernetes-service-endpoints"} == 0`,
			expected: []string{`up{job="kubernetes-service-endpoints"}`},
		},
		{
			name:     "not equals operator",
			expr:     `kube_pod_status_ready{condition="true"} != 1`,
			expected: []string{`kube_pod_status_ready{condition="true"}`},
		},
		{
			name:     "no operator",
			expr:     "sum(rate(requests_total[5m]))",
			expected: []string{"sum(rate(requests_total[5m]))"},
		},
		{
			name:     "complex query with offset and functions",
			expr:     `(kube_pod_container_status_restarts_total - kube_pod_container_status_restarts_total offset 10m >= 1) and ignoring (reason) min_over_time(kube_pod_container_status_last_terminated_reason{container="main",pod=~"podname.*",reason="OOMKilled"}[10m]) == 1`,
			expected: []string{""}, // todo: not supported
		},
		{
			name:     "query with OR operator",
			expr:     `(node_memory_MemAvailable_bytes < 1000000) or (node_memory_MemFree_bytes < 1000000)`,
			expected: []string{""}, // todo: not supported
		},
		{
			name:     "query with unless operator",
			expr:     `rate(http_requests_total[5m]) > 100 unless on(instance) up == 0`,
			expected: []string{""}, // todo: not supported
		},
		{
			name:     "query with unless operator",
			expr:     `sum(rate(http_requests_total{code=~"5.."}[5m])) / sum(rate(http_requests_total[5m])) > 0.1`,
			expected: []string{`sum(rate(http_requests_total{code=~"5.."}[5m])) / sum(rate(http_requests_total[5m]))`},
		},
		{
			name:     "query with by clause",
			expr:     `sum by(code) (rate(http_requests_total[5m])) > 100`,
			expected: []string{"sum by(code) (rate(http_requests_total[5m]))"},
		},
		{
			name:     "query with without clause",
			expr:     `sum without(instance) (rate(errors[5m])) >= 10`,
			expected: []string{"sum without(instance) (rate(errors[5m]))"},
		},
		{
			name:     "query with regex matching",
			expr:     `rate(http_requests{status=~"5.."}[5m]) > 0`,
			expected: []string{`rate(http_requests{status=~"5.."}[5m])`},
		},
		{
			name:     "query with subquery",
			expr:     `max_over_time(rate(http_requests_total[5m])[1h:]) > 100`,
			expected: []string{"max_over_time(rate(http_requests_total[5m])[1h:])"},
		},
		{
			name:     "query with offset and bool modifier",
			expr:     `(rate(errors[5m] offset 1h) > bool 0) == 1`,
			expected: []string{"rate(errors[5m] offset 1h) > bool 0"},
		},
		{
			name:     "query with multiple aggregations",
			expr:     `avg(sum by(instance) (rate(requests_total[5m]))) > 100`,
			expected: []string{"avg(sum by(instance) (rate(requests_total[5m])))"},
		},
	}

	r := &AlertDashboardReconciler{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := r.extractBaseQuery(&monitoringv1.Rule{
				Expr: intstr.FromString(tt.expr),
			})
			for i, result := range results {
				// empty string is a valid result, so we skip it
				if result == "" && tt.expected[i] == "" {
					continue
				}
				// parse the expected expression to compare with the result from parser
				expectedExpr, err := parser.ParseExpr(tt.expected[i])
				require.NoError(t, err, "Failed to parse expected expression")
				assert.Equal(t, expectedExpr.String(), result, "Extracted query does not match expected")
			}
		})
	}
}

func TestExtractBaseQuerySimple(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		expected []string
	}{
		{
			name:     "greater than operator",
			expr:     "sum(rate(http_requests_total[5m])) > 100",
			expected: []string{"sum(rate(http_requests_total[5m]))"},
		},
		{
			name:     "empty string",
			expr:     "",
			expected: []string{""},
		},
	}

	r := &AlertDashboardReconciler{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := r.extractBaseQuery(&monitoringv1.Rule{
				Expr: intstr.FromString(tt.expr),
			})
			for i, result := range results {
				// empty string is a valid result, so we skip it
				if result == "" && tt.expected[i] == "" {
					continue
				}
				// parse the expected expression to compare with the result from parser
				expectedExpr, err := parser.ParseExpr(tt.expected[i])
				require.NoError(t, err, "Failed to parse expected expression")
				assert.Equal(t, expectedExpr.String(), result, "Extracted query does not match expected")
			}
		})
	}
}

func TestExtractBaseQueryInvalidExpr(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{
			name: "invalid prometheus query",
			expr: "sum(rate(invalid metric[5m]) >>",
		},
		{
			name: "malformed query",
			expr: "rate(http_requests{[5m])",
		},
	}

	r := &AlertDashboardReconciler{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := r.extractBaseQuery(&monitoringv1.Rule{
				Expr: intstr.FromString(tt.expr),
			})
			assert.Equal(t, []string{""}, results, "Invalid query should return empty string array")
		})
	}
}

func TestExtractBaseQueryMultiCondition(t *testing.T) {
	// todo: not supported
	tests := []struct {
		name     string
		expr     string
		expected []string
	}{
		{
			name:     "complex query with offset and functions",
			expr:     `(kube_pod_container_status_restarts_total - kube_pod_container_status_restarts_total offset 10m >= 1) and ignoring (reason) min_over_time(kube_pod_container_status_last_terminated_reason{container="main",pod=~"podname.*",reason="OOMKilled"}[10m]) == 1`,
			expected: []string{""},
		},
		{
			name:     "query with offset and bool modifier",
			expr:     `(instance:node_cpu_utilization:rate5m > 0.9) and (rate(http_requests_total[5m]) < 10)`,
			expected: []string{""},
		},
	}

	r := &AlertDashboardReconciler{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := r.extractBaseQuery(&monitoringv1.Rule{
				Expr: intstr.FromString(tt.expr),
			})
			for i, result := range results {
				// parse the expected expression to compare with the result from parser
				expectedExpr, err := parser.ParseExpr(tt.expected[i])
				require.NoError(t, err, "Failed to parse expected expression")
				assert.Equal(t, expectedExpr.String(), result, "Extracted query does not match expected")
			}
		})
	}
}

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
	return wait.PollImmediate(time.Second, time.Second*10, func() (bool, error) {
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
