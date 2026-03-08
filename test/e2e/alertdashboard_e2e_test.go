/*
Licensed under MIT License. See LICENSE file in the root directory of this repository.
*/

package e2e

import (
	"context"
	"fmt"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	"github.com/krutsko/alert2dash-operator/internal/constants"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// testNamespace creates an isolated namespace for a test and returns a cleanup func.
func testNamespace(testCtx context.Context, name string) (string, func()) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	Expect(k8sClient.Create(testCtx, ns)).To(Succeed())
	return name, func() {
		_ = k8sClient.Delete(testCtx, ns)
	}
}

var _ = Describe("AlertDashboard E2E", func() {
	const (
		timeout  = "30s"
		interval = "1s"
	)

	Describe("ConfigMap lifecycle", func() {
		It("creates a ConfigMap with dashboard JSON when a PrometheusRule matches", func() {
			testCtx := context.Background()
			ns, cleanup := testNamespace(testCtx, "e2e-create")
			defer cleanup()

			By("creating a PrometheusRule")
			rule := prometheusRule(ns, "test-rule", map[string]string{"app": "svc"}, []monitoringv1.RuleGroup{
				{
					Name: "alerts",
					Rules: []monitoringv1.Rule{
						{
							Alert:  "HighCPU",
							Expr:   intstr.FromString("cpu_usage > 80"),
							Labels: map[string]string{"severity": "critical"},
						},
					},
				},
			})
			Expect(k8sClient.Create(testCtx, rule)).To(Succeed())

			By("creating an AlertDashboard that selects the rule")
			dashboard := alertDashboard(ns, &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "svc"},
			}, nil)
			Expect(k8sClient.Create(testCtx, dashboard)).To(Succeed())

			By("waiting for the ConfigMap to be created")
			cmName := fmt.Sprintf("grafana-dashboard-%s", dashboard.Name)
			Eventually(func() error {
				cm := &corev1.ConfigMap{}
				return k8sClient.Get(testCtx, types.NamespacedName{Name: cmName, Namespace: ns}, cm)
			}, timeout, interval).Should(Succeed())

			By("verifying ConfigMap has Grafana labels and dashboard JSON")
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(testCtx, types.NamespacedName{Name: cmName, Namespace: ns}, cm)).To(Succeed())
			Expect(cm.Labels[constants.LabelGrafanaDashboard]).To(Equal("1"))
			Expect(cm.Labels[constants.LabelDashboardName]).To(Equal(dashboard.Name))
			Expect(cm.Data).To(HaveKey(dashboard.Name + ".json"))
			Expect(cm.Data[dashboard.Name+".json"]).To(ContainSubstring("HighCPU"))
		})

		It("updates the ConfigMap when the PrometheusRule spec changes", func() {
			testCtx := context.Background()
			ns, cleanup := testNamespace(testCtx, "e2e-update")
			defer cleanup()

			By("creating a PrometheusRule and AlertDashboard")
			rule := prometheusRule(ns, "test-rule", map[string]string{"app": "svc"}, []monitoringv1.RuleGroup{
				{
					Name: "alerts",
					Rules: []monitoringv1.Rule{
						{Alert: "AlertV1", Expr: intstr.FromString("metric_v1 > 10"), Labels: map[string]string{}},
					},
				},
			})
			Expect(k8sClient.Create(testCtx, rule)).To(Succeed())

			dashboard := alertDashboard(ns, &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "svc"},
			}, nil)
			Expect(k8sClient.Create(testCtx, dashboard)).To(Succeed())

			cmName := fmt.Sprintf("grafana-dashboard-%s", dashboard.Name)
			By("waiting for the initial ConfigMap")
			Eventually(func() bool {
				cm := &corev1.ConfigMap{}
				if err := k8sClient.Get(testCtx, types.NamespacedName{Name: cmName, Namespace: ns}, cm); err != nil {
					return false
				}
				return cm.Data[dashboard.Name+".json"] != ""
			}, timeout, interval).Should(BeTrue())

			By("updating the PrometheusRule with a new alert")
			Expect(k8sClient.Get(testCtx, types.NamespacedName{Name: rule.Name, Namespace: ns}, rule)).To(Succeed())
			rule.Spec.Groups[0].Rules = []monitoringv1.Rule{
				{Alert: "AlertV2", Expr: intstr.FromString("metric_v2 > 20"), Labels: map[string]string{}},
			}
			Expect(k8sClient.Update(testCtx, rule)).To(Succeed())

			By("waiting for the ConfigMap to reflect the updated alert")
			Eventually(func() bool {
				cm := &corev1.ConfigMap{}
				if err := k8sClient.Get(testCtx, types.NamespacedName{Name: cmName, Namespace: ns}, cm); err != nil {
					return false
				}
				return cm.Data[dashboard.Name+".json"] != "" &&
					contains(cm.Data[dashboard.Name+".json"], "AlertV2")
			}, timeout, interval).Should(BeTrue())
		})

	})

	Describe("Label selector filtering", func() {
		It("only includes alerts matching the RuleLabelSelector", func() {
			testCtx := context.Background()
			ns, cleanup := testNamespace(testCtx, "e2e-ruleselector")
			defer cleanup()

			By("creating a PrometheusRule with alerts of different severities")
			rule := prometheusRule(ns, "test-rule", map[string]string{"app": "svc"}, []monitoringv1.RuleGroup{
				{
					Name: "alerts",
					Rules: []monitoringv1.Rule{
						{
							Alert:  "CriticalAlert",
							Expr:   intstr.FromString("crit_metric > 90"),
							Labels: map[string]string{"severity": "critical"},
						},
						{
							Alert:  "WarningAlert",
							Expr:   intstr.FromString("warn_metric > 70"),
							Labels: map[string]string{"severity": "warning"},
						},
					},
				},
			})
			Expect(k8sClient.Create(testCtx, rule)).To(Succeed())

			By("creating an AlertDashboard that selects only critical alerts")
			dashboard := alertDashboard(ns, &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "svc"},
			}, &metav1.LabelSelector{
				MatchLabels: map[string]string{"severity": "critical"},
			})
			Expect(k8sClient.Create(testCtx, dashboard)).To(Succeed())

			cmName := fmt.Sprintf("grafana-dashboard-%s", dashboard.Name)
			By("waiting for ConfigMap")
			Eventually(func() error {
				return k8sClient.Get(testCtx, types.NamespacedName{Name: cmName, Namespace: ns}, &corev1.ConfigMap{})
			}, timeout, interval).Should(Succeed())

			By("verifying only the critical alert appears in the dashboard")
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(testCtx, types.NamespacedName{Name: cmName, Namespace: ns}, cm)).To(Succeed())
			dashJson := cm.Data[dashboard.Name+".json"]
			Expect(dashJson).To(ContainSubstring("CriticalAlert"))
			Expect(dashJson).NotTo(ContainSubstring("WarningAlert"))
		})

		It("does not create a ConfigMap when no PrometheusRules match the MetadataLabelSelector", func() {
			testCtx := context.Background()
			ns, cleanup := testNamespace(testCtx, "e2e-nomatch")
			defer cleanup()

			By("creating a PrometheusRule with labels that don't match the selector")
			rule := prometheusRule(ns, "test-rule", map[string]string{"app": "other"}, []monitoringv1.RuleGroup{
				{
					Name:  "alerts",
					Rules: []monitoringv1.Rule{{Alert: "SomeAlert", Expr: intstr.FromString("m > 1"), Labels: map[string]string{}}},
				},
			})
			Expect(k8sClient.Create(testCtx, rule)).To(Succeed())

			By("creating an AlertDashboard selecting a different app label")
			dashboard := alertDashboard(ns, &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "svc"},
			}, nil)
			Expect(k8sClient.Create(testCtx, dashboard)).To(Succeed())

			By("verifying that a finalizer is added (controller is active)")
			Eventually(func() bool {
				d := &monitoringv1alpha1.AlertDashboard{}
				if err := k8sClient.Get(testCtx, types.NamespacedName{Name: dashboard.Name, Namespace: ns}, d); err != nil {
					return false
				}
				return slices.Contains(d.Finalizers, "alert2dash.monitoring.krutsko/finalizer")
			}, timeout, interval).Should(BeTrue())

			By("verifying no ConfigMap is created")
			cmName := fmt.Sprintf("grafana-dashboard-%s", dashboard.Name)
			Consistently(func() bool {
				err := k8sClient.Get(testCtx, types.NamespacedName{Name: cmName, Namespace: ns}, &corev1.ConfigMap{})
				return client.IgnoreNotFound(err) == nil && err != nil // true means NotFound
			}, "5s", interval).Should(BeTrue())
		})

		It("skips alerts with the exclude label", func() {
			testCtx := context.Background()
			ns, cleanup := testNamespace(testCtx, "e2e-exclude")
			defer cleanup()

			By("creating a PrometheusRule with one excluded and one normal alert")
			rule := prometheusRule(ns, "test-rule", map[string]string{"app": "svc"}, []monitoringv1.RuleGroup{
				{
					Name: "alerts",
					Rules: []monitoringv1.Rule{
						{
							Alert:  "NormalAlert",
							Expr:   intstr.FromString("normal_metric > 5"),
							Labels: map[string]string{},
						},
						{
							Alert:  "ExcludedAlert",
							Expr:   intstr.FromString("excluded_metric > 5"),
							Labels: map[string]string{constants.LabelExcludeRule: "true"},
						},
					},
				},
			})
			Expect(k8sClient.Create(testCtx, rule)).To(Succeed())

			dashboard := alertDashboard(ns, &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "svc"},
			}, nil)
			Expect(k8sClient.Create(testCtx, dashboard)).To(Succeed())

			cmName := fmt.Sprintf("grafana-dashboard-%s", dashboard.Name)
			Eventually(func() error {
				return k8sClient.Get(testCtx, types.NamespacedName{Name: cmName, Namespace: ns}, &corev1.ConfigMap{})
			}, timeout, interval).Should(Succeed())

			By("verifying the excluded alert is absent from the dashboard")
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(testCtx, types.NamespacedName{Name: cmName, Namespace: ns}, cm)).To(Succeed())
			dashJson := cm.Data[dashboard.Name+".json"]
			Expect(dashJson).To(ContainSubstring("NormalAlert"))
			Expect(dashJson).NotTo(ContainSubstring("ExcludedAlert"))
		})
	})

	Describe("Status updates", func() {
		It("populates status with ConfigMapName and ObservedRules after reconciliation", func() {
			testCtx := context.Background()
			ns, cleanup := testNamespace(testCtx, "e2e-status")
			defer cleanup()

			rule := prometheusRule(ns, "status-rule", map[string]string{"app": "svc"}, []monitoringv1.RuleGroup{
				{
					Name:  "alerts",
					Rules: []monitoringv1.Rule{{Alert: "StatusAlert", Expr: intstr.FromString("status_metric > 1"), Labels: map[string]string{}}},
				},
			})
			Expect(k8sClient.Create(testCtx, rule)).To(Succeed())

			dashboard := alertDashboard(ns, &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "svc"},
			}, nil)
			Expect(k8sClient.Create(testCtx, dashboard)).To(Succeed())

			By("waiting for status to be set")
			Eventually(func() string {
				d := &monitoringv1alpha1.AlertDashboard{}
				if err := k8sClient.Get(testCtx, types.NamespacedName{Name: dashboard.Name, Namespace: ns}, d); err != nil {
					return ""
				}
				return d.Status.ConfigMapName
			}, timeout, interval).ShouldNot(BeEmpty())

			By("verifying status fields")
			d := &monitoringv1alpha1.AlertDashboard{}
			Expect(k8sClient.Get(testCtx, types.NamespacedName{Name: dashboard.Name, Namespace: ns}, d)).To(Succeed())
			Expect(d.Status.ConfigMapName).To(Equal(fmt.Sprintf("grafana-dashboard-%s", dashboard.Name)))
			Expect(d.Status.ObservedRules).To(ContainElement(rule.Name))
			Expect(d.Status.RulesHash).NotTo(BeEmpty())
			Expect(d.Status.LastUpdated).NotTo(BeEmpty())
		})
	})
})

// helpers

func prometheusRule(ns, name string, labels map[string]string, groups []monitoringv1.RuleGroup) *monitoringv1.PrometheusRule {
	return &monitoringv1.PrometheusRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: monitoringv1.PrometheusRuleSpec{Groups: groups},
	}
}

func alertDashboard(ns string, metaSelector, ruleSelector *metav1.LabelSelector) *monitoringv1alpha1.AlertDashboard {
	return &monitoringv1alpha1.AlertDashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-dashboard",
			Namespace: ns,
		},
		Spec: monitoringv1alpha1.AlertDashboardSpec{
			MetadataLabelSelector: metaSelector,
			RuleLabelSelector:     ruleSelector,
			DashboardConfig: monitoringv1alpha1.DashboardConfig{
				ConfigMapNamePrefix: "grafana-dashboard",
			},
		},
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
