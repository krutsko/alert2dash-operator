/*
Licensed under MIT License. See LICENSE file in the root directory of this repository.
*/

package controller

import (
	"context"
	"fmt"
	"reflect"
	"strings"
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
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/go-logr/logr"
	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	"github.com/krutsko/alert2dash-operator/internal/constants"
	"github.com/krutsko/alert2dash-operator/internal/model"
	"github.com/krutsko/alert2dash-operator/internal/utils"
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
					MetadataLabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app":                "test-app",
							"generate-dashboard": "true",
						},
					},
					RuleLabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"team": "dreamteam",
							"tier": "backend",
						},
					},
					DashboardConfig: monitoringv1alpha1.DashboardConfig{
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
				return utils.HasString(dashboard.Finalizers, dashboardFinalizer)
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
				if configMap.Labels[constants.LabelGrafanaDashboard] != "1" {
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
			Expect(configMap.Labels[constants.LabelGrafanaDashboard]).To(Equal("1"))

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
				return utils.HasString(dashboard.Finalizers, dashboardFinalizer)
			}, "10s", "1s").Should(BeTrue())

			// Second reconciliation - should process dashboard
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the ConfigMap has correct owner references")
			var configMap *corev1.ConfigMap
			Eventually(func() error {
				configMap = &corev1.ConfigMap{}
				err = k8sClient.Get(ctx, types.NamespacedName{
					Name:      "grafana-dashboard-" + resourceName,
					Namespace: "default",
				}, configMap)

				if err != nil {
					return err
				}

				// Just get the configmap and store it for later checks
				return nil
			}, "10s", "1s").Should(Succeed())

			// Now verify it has proper owner references
			Expect(configMap.OwnerReferences).To(HaveLen(1))
			Expect(configMap.OwnerReferences[0].Kind).To(Equal("AlertDashboard"))
			Expect(configMap.OwnerReferences[0].Name).To(Equal(resourceName))

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

			By("verifying the finalizer was removed")
			Eventually(func() bool {
				dashboard := &monitoringv1alpha1.AlertDashboard{}
				err := k8sClient.Get(ctx, namespacedName, dashboard)
				if err != nil && kuberr.IsNotFound(err) {
					return true
				}
				if err != nil {
					return false
				}
				return len(dashboard.Finalizers) == 0
			}, "10s", "1s").Should(BeTrue())
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
					MetadataLabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"non-existent": "label",
						},
					},
					RuleLabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"non-existent": "label",
						},
					},
					DashboardConfig: monitoringv1alpha1.DashboardConfig{
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

		It("should handle excluded rules correctly", func() {
			By("creating test PrometheusRule with exclude label")
			const testResourceWithExcludedRule = "test-resource-with-excluded-rule"
			By("creating the custom resource without matching PrometheusRules")
			alertDashboardWithExcludedRule := &monitoringv1alpha1.AlertDashboard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testResourceWithExcludedRule,
					Namespace: "default",
				},
				Spec: monitoringv1alpha1.AlertDashboardSpec{
					MetadataLabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app":                "test-app",
							"generate-dashboard": "true",
						},
					},
					RuleLabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test-app",
						},
					},
					DashboardConfig: monitoringv1alpha1.DashboardConfig{
						ConfigMapNamePrefix: "grafana-dashboard",
					},
				},
			}
			Expect(k8sClient.Create(ctx, alertDashboardWithExcludedRule)).To(Succeed())
			excludedRules := &monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "excluded-rule",
					Namespace: "default",
					Labels:    map[string]string{"app": "test-app", "generate-dashboard": "true"},
				},
				Spec: monitoringv1.PrometheusRuleSpec{
					Groups: []monitoringv1.RuleGroup{
						{
							Name: "excluded.rules.group",
							Rules: []monitoringv1.Rule{
								{
									Alert: "ExcludedAlert1",
									Expr:  intstr.FromString("vector(1) == 1"),
									Labels: map[string]string{
										"severity": "warning", // no required labels
									},
								},
								{
									Alert: "ExcludedAlert2",
									Expr:  intstr.FromString("vector(1) == 1"),
									Labels: map[string]string{
										"app":                      "test-app",
										constants.LabelExcludeRule: "true", // This is the label that should exlude the rule from the dashboard
									},
								},
								{
									Alert: "GoodAlert",
									Expr:  intstr.FromString("vector(1) == 1"),
									Labels: map[string]string{
										"severity": "warning",
										"app":      "test-app",
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, excludedRules)).To(Succeed())

			By("triggering a reconciliation")
			reconciler := NewAlertDashboardReconciler(
				k8sClient,
				k8sClient.Scheme(),
				ctrl.Log.WithName("controllers").WithName("test"),
			)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      testResourceWithExcludedRule,
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait for finalizer to be added and verify ConfigMap creation
			Eventually(func() bool {
				alertDashboardWithExcludedRule := &monitoringv1alpha1.AlertDashboard{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      testResourceWithExcludedRule,
					Namespace: "default",
				}, alertDashboardWithExcludedRule)
				if err != nil {
					return false
				}
				return utils.HasString(alertDashboardWithExcludedRule.Finalizers, dashboardFinalizer)
			}, "10s", "1s").Should(BeTrue())

			// Second reconciliation - should process dashboard
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      testResourceWithExcludedRule,
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the ConfigMap doesn't contain excluded alert")
			configMap := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "grafana-dashboard-" + testResourceWithExcludedRule,
					Namespace: "default",
				}, configMap)
			}, "10s", "1s").Should(Succeed())

			dashboardJson := configMap.Data[testResourceWithExcludedRule+".json"]

			// Verify panel exclusion
			Expect(dashboardJson).NotTo(ContainSubstring(`"title": "ExcludedAlert1"`))
			Expect(dashboardJson).NotTo(ContainSubstring(`"title": "ExcludedAlert2"`))

			// Verify included panel
			Expect(dashboardJson).To(ContainSubstring(`"title": "GoodAlert"`))
			Expect(dashboardJson).To(ContainSubstring(`"expr": "vector(1)"`))

			By("cleaning up the excluded rule")
			err = handleResourceDeletion(ctx, k8sClient, excludedRules, types.NamespacedName{
				Name:      "excluded-rule",
				Namespace: "default",
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should update dashboard when rule is marked as excluded", func() {
			By("triggering initial reconciliation")
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

			// Second reconciliation - should process dashboard
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying initial dashboard contains both alerts")
			configMap := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "grafana-dashboard-" + resourceName,
					Namespace: "default",
				}, configMap)
			}, "10s", "1s").Should(Succeed())

			initialDashboardJson := configMap.Data[resourceName+".json"]
			Expect(initialDashboardJson).To(ContainSubstring(`"title": "HighErrorRate"`))
			Expect(initialDashboardJson).To(ContainSubstring(`"title": "HighLatency"`))

			By("marking one rule as excluded")
			prometheusRule := &monitoringv1.PrometheusRule{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-rule",
				Namespace: "default",
			}, prometheusRule)).To(Succeed())

			// Add exclude label to the first rule
			prometheusRule.Spec.Groups[0].Rules[0].Labels[constants.LabelExcludeRule] = "true"
			Expect(k8sClient.Update(ctx, prometheusRule)).To(Succeed())

			By("triggering reconciliation after rule exclusion")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying dashboard was updated to exclude marked rule")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "grafana-dashboard-" + resourceName,
					Namespace: "default",
				}, configMap); err != nil {
					return false
				}
				updatedDashboardJson := configMap.Data[resourceName+".json"]
				return !strings.Contains(updatedDashboardJson, `"title": "HighErrorRate"`) &&
					strings.Contains(updatedDashboardJson, `"title": "HighLatency"`)
			}, "10s", "1s").Should(BeTrue(), "Dashboard should only contain non-excluded rule")
		})

		It("should update dashboard when PrometheusRule metadata labels change", func() {
			By("triggering initial reconciliation")
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

			// Second reconciliation - should process dashboard
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying initial dashboard exists")
			configMap := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "grafana-dashboard-" + resourceName,
					Namespace: "default",
				}, configMap)
			}, "10s", "1s").Should(Succeed())

			By("modifying PrometheusRule metadata labels")
			modifiedRule := &monitoringv1.PrometheusRule{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-rule",
				Namespace: "default",
			}, modifiedRule)).To(Succeed())

			// Change a relevant metadata label that should trigger an update
			modifiedRule.Labels["generate-dashboard"] = "false"
			Expect(k8sClient.Update(ctx, modifiedRule)).To(Succeed())

			By("triggering reconciliation after label change")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying dashboard was updated")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "grafana-dashboard-" + resourceName,
					Namespace: "default",
				}, configMap); err != nil {
					return false
				}

				dashboardJson := configMap.Data[resourceName+".json"]
				return !strings.Contains(dashboardJson, `"title": "HighErrorRate"`) &&
					!strings.Contains(dashboardJson, `"title": "HighLatency"`)
			}, "10s", "1s").Should(BeTrue(), "Dashboard should not contain any panel titles after removing generate-dashboard label")
		})

		AfterEach(func() {
			By("cleaning up all PrometheusRules")
			prometheusRule := &monitoringv1.PrometheusRule{}
			// Clean up the main test rule
			err := handleResourceDeletion(ctx, k8sClient, prometheusRule, types.NamespacedName{
				Name:      "test-rule",
				Namespace: "default",
			})
			Expect(err).NotTo(HaveOccurred())

			// Clean up the excluded rule
			err = handleResourceDeletion(ctx, k8sClient, prometheusRule, types.NamespacedName{
				Name:      "excluded-rule",
				Namespace: "default",
			})
			Expect(err).NotTo(HaveOccurred())

			By("cleaning up all AlertDashboard resources")
			// Clean up the main dashboard
			alertDashboard := &monitoringv1alpha1.AlertDashboard{}
			err = handleResourceDeletion(ctx, k8sClient, alertDashboard, namespacedName)
			Expect(err).NotTo(HaveOccurred())

			// Clean up the additional dashboard
			err = handleResourceDeletion(ctx, k8sClient, alertDashboard, types.NamespacedName{
				Name:      "test-resource-with-excluded-rule",
				Namespace: "default",
			})
			Expect(err).NotTo(HaveOccurred())

			By("cleaning up all ConfigMaps")
			configMap := &corev1.ConfigMap{}
			// Clean up the main configmap
			err = handleResourceDeletion(ctx, k8sClient, configMap, types.NamespacedName{
				Name:      "grafana-dashboard-" + resourceName,
				Namespace: "default",
			})
			Expect(err).NotTo(HaveOccurred())

			// Clean up the additional configmap
			err = handleResourceDeletion(ctx, k8sClient, configMap, types.NamespacedName{
				Name:      "grafana-dashboard-test-resource-with-excluded-rule",
				Namespace: "default",
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

var _ = Describe("AlertDashboard Controller Rule Updates", func() {
	Context("When PrometheusRules are modified", func() {
		const resourceName = "test-resource-rule-updates"

		ctx := context.Background()

		var (
			reconciler *AlertDashboardReconciler
			baseRule   *monitoringv1.PrometheusRule
			dashboard  *monitoringv1alpha1.AlertDashboard
		)

		BeforeEach(func() {
			reconciler = NewAlertDashboardReconciler(
				k8sClient,
				k8sClient.Scheme(),
				ctrl.Log.WithName("controllers").WithName("test"),
			)

			duration := monitoringv1.Duration("5m")
			baseRule = &monitoringv1.PrometheusRule{
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
									Alert: "TestAlert",
									Expr:  intstr.FromString("up == 0"),
									For:   &duration,
									Labels: map[string]string{
										"severity": "critical",
										"team":     "dreamteam",
									},
								},
							},
						},
					},
				},
			}

			dashboard = &monitoringv1alpha1.AlertDashboard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: monitoringv1alpha1.AlertDashboardSpec{
					MetadataLabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app":                "test-app",
							"generate-dashboard": "true",
						},
					},
					RuleLabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"team": "dreamteam",
						},
					},
					DashboardConfig: monitoringv1alpha1.DashboardConfig{
						ConfigMapNamePrefix: "grafana-dashboard",
					},
				},
			}

			// Create resources
			Expect(k8sClient.Create(ctx, baseRule)).To(Succeed())
			Expect(k8sClient.Create(ctx, dashboard)).To(Succeed())
		})

		AfterEach(func() {
			// Cleanup
			Expect(handleResourceDeletion(ctx, k8sClient, baseRule, types.NamespacedName{
				Name:      "test-rule",
				Namespace: "default",
			})).To(Succeed())

			Expect(handleResourceDeletion(ctx, k8sClient, dashboard, types.NamespacedName{
				Name:      resourceName,
				Namespace: "default",
			})).To(Succeed())
		})

		It("should trigger updates on rule expression changes", func() {
			By("modifying the rule expression")
			modifiedRule := baseRule.DeepCopy()
			modifiedRule.Spec.Groups[0].Rules[0].Expr = intstr.FromString("up < 1")

			updateEvent := event.UpdateEvent{
				ObjectOld: baseRule,
				ObjectNew: modifiedRule,
			}

			predicate := &prometheusRulePredicate{}
			Expect(predicate.Update(updateEvent)).To(BeTrue())

			requests := reconciler.handlePrometheusRuleEvent(ctx, modifiedRule)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(resourceName))
			Expect(requests[0].Namespace).To(Equal("default"))
		})

		It("should trigger updates when adding new alert rules", func() {
			By("adding a new alert rule")
			modifiedRule := baseRule.DeepCopy()
			modifiedRule.Spec.Groups[0].Rules = append(modifiedRule.Spec.Groups[0].Rules, monitoringv1.Rule{
				Alert: "NewAlert",
				Expr:  intstr.FromString("rate(errors[5m]) > 0.1"),
				Labels: map[string]string{
					"severity": "warning",
					"team":     "dreamteam",
				},
			})

			updateEvent := event.UpdateEvent{
				ObjectOld: baseRule,
				ObjectNew: modifiedRule,
			}

			predicate := &prometheusRulePredicate{}
			Expect(predicate.Update(updateEvent)).To(BeTrue())

			requests := reconciler.handlePrometheusRuleEvent(ctx, modifiedRule)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(resourceName))
		})

		It("should not trigger updates for metadata-only changes", func() {
			By("modifying rule annotations")
			modifiedRule := baseRule.DeepCopy()
			if modifiedRule.Annotations == nil {
				modifiedRule.Annotations = make(map[string]string)
			}
			modifiedRule.Annotations["description"] = "Updated description"

			updateEvent := event.UpdateEvent{
				ObjectOld: baseRule,
				ObjectNew: modifiedRule,
			}

			predicate := &prometheusRulePredicate{}
			By("verifying predicate blocks metadata-only updates")
			Expect(predicate.Update(updateEvent)).To(BeFalse(), "Predicate should return false for metadata-only changes")

			// Skip event handling check since predicate returned false
			By("skipping event handling as predicate returned false")
		})

		It("should handle rule label changes correctly", func() {
			By("changing rule labels")
			modifiedRule := baseRule.DeepCopy()
			modifiedRule.Spec.Groups[0].Rules[0].Labels["severity"] = "warning"

			updateEvent := event.UpdateEvent{
				ObjectOld: baseRule,
				ObjectNew: modifiedRule,
			}

			predicate := &prometheusRulePredicate{}
			By("verifying predicate allows label changes")
			predicateResult := predicate.Update(updateEvent)
			Expect(predicateResult).To(BeTrue(), "Predicate should allow rule label changes")

			requests := reconciler.handlePrometheusRuleEvent(ctx, modifiedRule)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(resourceName))

		})

		It("should handle multiple affected dashboards", func() {
			By("creating another dashboard that matches the rule")
			anotherDashboard := dashboard.DeepCopy()
			// Clear metadata fields that should not be set on creation
			anotherDashboard.ObjectMeta = metav1.ObjectMeta{
				Name:      "another-dashboard",
				Namespace: "default",
			}
			// Copy only the spec
			anotherDashboard.Spec = dashboard.Spec

			Expect(k8sClient.Create(ctx, anotherDashboard)).To(Succeed())

			By("modifying the rule")
			modifiedRule := baseRule.DeepCopy()
			modifiedRule.Spec.Groups[0].Rules[0].Expr = intstr.FromString("up < 1")

			updateEvent := event.UpdateEvent{
				ObjectOld: baseRule,
				ObjectNew: modifiedRule,
			}

			predicate := &prometheusRulePredicate{}
			By("verifying predicate allows label changes")
			predicateResult := predicate.Update(updateEvent)
			Expect(predicateResult).To(BeTrue(), "Predicate should allow rule label changes")

			requests := reconciler.handlePrometheusRuleEvent(ctx, modifiedRule)
			Expect(requests).To(HaveLen(2))

			// Cleanup
			Expect(handleResourceDeletion(ctx, k8sClient, anotherDashboard, types.NamespacedName{
				Name:      "another-dashboard",
				Namespace: "default",
			})).To(Succeed())
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

func TestGetGrafanaThresholdOperator(t *testing.T) {
	// Create a minimal reconciler for testing
	reconciler := &AlertDashboardReconciler{
		Log: logr.Discard(), // Use a no-op logger for tests
	}

	tests := []struct {
		name     string
		operator parser.ItemType
		want     string
	}{
		{
			name:     "equal operator",
			operator: parser.EQL,
			want:     "gt",
		},
		{
			name:     "not equal operator",
			operator: parser.NEQ,
			want:     "gt",
		},
		{
			name:     "greater than operator",
			operator: parser.GTR,
			want:     "gt",
		},
		{
			name:     "greater than or equal operator",
			operator: parser.GTE,
			want:     "gt",
		},
		{
			name:     "less than operator",
			operator: parser.LSS,
			want:     "lt",
		},
		{
			name:     "less than or equal operator",
			operator: parser.LTE,
			want:     "lt",
		},
		{
			name:     "unsupported operator",
			operator: parser.ADD,
			want:     "gt", // Falls back to "gt" for unsupported operators
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reconciler.getGrafanaThresholdOperator(tt.operator)
			if got != tt.want {
				t.Errorf("getGrafanaThresholdOperator() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSanitizeExpr(t *testing.T) {
	reconciler := &AlertDashboardReconciler{
		Log: logr.Discard(), // Use a no-op logger for tests
	}

	tests := []struct {
		name      string
		expr      string
		threshold string
		operator  parser.ItemType
		want      model.ParsedQueryResult
	}{
		{
			name:      "valid expression and threshold",
			expr:      "rate(http_requests_total[5m])",
			threshold: "100",
			operator:  parser.GTR,
			want: model.ParsedQueryResult{
				Query:     "rate(http_requests_total[5m])",
				Threshold: 100,
				Operator:  "gt",
			},
		},
		{
			name:      "expression with parentheses",
			expr:      "(rate(http_requests_total[5m]))",
			threshold: "50.5",
			operator:  parser.LSS,
			want: model.ParsedQueryResult{
				Query:     "rate(http_requests_total[5m])",
				Threshold: 50.5,
				Operator:  "lt",
			},
		},
		{
			name:      "invalid threshold",
			expr:      "rate(http_requests_total[5m])",
			threshold: "invalid",
			operator:  parser.GTR,
			want:      model.ParsedQueryResult{}, // Should return empty result for invalid threshold
		},
		{
			name:      "expression with multiple parentheses",
			expr:      "(((rate(http_requests_total[5m]))))",
			threshold: "75",
			operator:  parser.GTE,
			want: model.ParsedQueryResult{
				Query:     "rate(http_requests_total[5m])",
				Threshold: 75,
				Operator:  "gt",
			},
		},
		{
			name:      "expression with whitespace",
			expr:      "  rate(http_requests_total[5m])  ",
			threshold: "25",
			operator:  parser.LTE,
			want: model.ParsedQueryResult{
				Query:     "rate(http_requests_total[5m])",
				Threshold: 25,
				Operator:  "lt",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reconciler.sanitizeExpr(tt.expr, tt.threshold, tt.operator)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("sanitizeExpr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAlertDashboardReconciler_extractQuery(t *testing.T) {
	// Create a reconciler instance for testing
	r := &AlertDashboardReconciler{
		Log: logr.Discard(), // Use a no-op logger for tests
	}

	tests := []struct {
		name     string
		expr     string
		expected []model.ParsedQueryResult
	}{
		{
			name: "simple comparison",
			expr: "metric > 5",
			expected: []model.ParsedQueryResult{
				{
					Query:     "metric",
					Threshold: 5,
					Operator:  "gt",
				},
			},
		},
		{
			name: "parenthesized expression",
			expr: "(metric > 10)",
			expected: []model.ParsedQueryResult{
				{
					Query:     "metric",
					Threshold: 10,
					Operator:  "gt",
				},
			},
		},
		{
			name: "nested parentheses",
			expr: "((metric > 15))",
			expected: []model.ParsedQueryResult{
				{
					Query:     "metric",
					Threshold: 15,
					Operator:  "gt",
				},
			},
		},
		{
			name:     "logical operator",
			expr:     "metric1 > 5 and metric2 < 10",
			expected: []model.ParsedQueryResult{},
		},
		{
			name:     "invalid expression",
			expr:     "invalid >>>>",
			expected: []model.ParsedQueryResult{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.extractQuery(tt.expr)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d results, got %d", len(tt.expected), len(result))
				return
			}

			for i, exp := range tt.expected {
				if !reflect.DeepEqual(result[i], exp) {
					t.Errorf("result[%d] = %+v, want %+v", i, result[i], exp)
				}
			}
		})
	}
}

func TestAlertDashboardReconciler_inverse(t *testing.T) {
	r := &AlertDashboardReconciler{}

	tests := []struct {
		name string
		op   parser.ItemType
		want parser.ItemType
	}{
		{
			name: "GTR -> LSS",
			op:   parser.GTR,
			want: parser.LSS,
		},
		{
			name: "LSS -> GTR",
			op:   parser.LSS,
			want: parser.GTR,
		},
		{
			name: "GTE -> LTE",
			op:   parser.GTE,
			want: parser.LTE,
		},
		{
			name: "LTE -> GTE",
			op:   parser.LTE,
			want: parser.GTE,
		},
		{
			name: "EQL remains EQL",
			op:   parser.EQL,
			want: parser.EQL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.inverse(tt.op)
			if got != tt.want {
				t.Errorf("inverse(%v) = %v, want %v", tt.op, got, tt.want)
			}
		})
	}
}
