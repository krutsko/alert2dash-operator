/*
Licensed under MIT License. See LICENSE file in the root directory of this repository.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AlertDashboardSpec defines the desired state of AlertDashboard
type AlertDashboardSpec struct {
	// MetadataLabelSelector is used to select PrometheusRules based on their metadata labels.
	// This selector filters at the PrometheusRule resource level.
	// +optional
	MetadataLabelSelector *metav1.LabelSelector `json:"metadataLabelSelector,omitempty"`

	// RuleLabelSelector defines criteria for selecting specific alert rules within PrometheusRules.
	// This selector filters individual alert rules based on their labels.
	// +optional
	RuleLabelSelector *metav1.LabelSelector `json:"ruleLabelSelector,omitempty"`

	// DashboardConfig defines the Grafana dashboard configuration
	DashboardConfig DashboardConfig `json:"dashboardConfig"`

	// Optional: Custom Jsonnet template used to generate the dashboard
	// +optional
	CustomJsonnetTemplate string `json:"customJsonnetTemplate,omitempty"`
}

type DashboardConfig struct {
	// Folder in Grafana where dashboard will be stored
	Folder string `json:"folder,omitempty"`

	// ConfigMapNamePrefix for the generated ConfigMap
	ConfigMapNamePrefix string `json:"configMapNamePrefix,omitempty"`
}

// AlertDashboardStatus defines the observed state of AlertDashboard
type AlertDashboardStatus struct {
	// ConfigMapName stores the name of generated ConfigMap
	ConfigMapName string `json:"configMapName,omitempty"`

	// LastUpdated timestamp of last successful update
	LastUpdated string `json:"lastUpdated,omitempty"`

	// ObservedRules list of PrometheusRules being watched
	ObservedRules []string `json:"observedRules,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AlertDashboard is the Schema for the alertdashboards API
type AlertDashboard struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AlertDashboardSpec   `json:"spec,omitempty"`
	Status AlertDashboardStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AlertDashboardList contains a list of AlertDashboard
type AlertDashboardList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AlertDashboard `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AlertDashboard{}, &AlertDashboardList{})
}
