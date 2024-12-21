//go:build !ignore_autogenerated

/*
Copyright (c) 2024

Licensed under MIT License. See LICENSE file in the root directory of this repository.
*/

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AlertDashboard) DeepCopyInto(out *AlertDashboard) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AlertDashboard.
func (in *AlertDashboard) DeepCopy() *AlertDashboard {
	if in == nil {
		return nil
	}
	out := new(AlertDashboard)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *AlertDashboard) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AlertDashboardList) DeepCopyInto(out *AlertDashboardList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]AlertDashboard, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AlertDashboardList.
func (in *AlertDashboardList) DeepCopy() *AlertDashboardList {
	if in == nil {
		return nil
	}
	out := new(AlertDashboardList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *AlertDashboardList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AlertDashboardSpec) DeepCopyInto(out *AlertDashboardSpec) {
	*out = *in
	if in.RuleSelector != nil {
		in, out := &in.RuleSelector, &out.RuleSelector
		*out = new(v1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
	in.DashboardConfig.DeepCopyInto(&out.DashboardConfig)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AlertDashboardSpec.
func (in *AlertDashboardSpec) DeepCopy() *AlertDashboardSpec {
	if in == nil {
		return nil
	}
	out := new(AlertDashboardSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AlertDashboardStatus) DeepCopyInto(out *AlertDashboardStatus) {
	*out = *in
	if in.ObservedRules != nil {
		in, out := &in.ObservedRules, &out.ObservedRules
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AlertDashboardStatus.
func (in *AlertDashboardStatus) DeepCopy() *AlertDashboardStatus {
	if in == nil {
		return nil
	}
	out := new(AlertDashboardStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DashboardConfig) DeepCopyInto(out *DashboardConfig) {
	*out = *in
	if in.Panels != nil {
		in, out := &in.Panels, &out.Panels
		*out = new(PanelConfig)
		**out = **in
	}
	if in.Variables != nil {
		in, out := &in.Variables, &out.Variables
		*out = make([]Variable, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DashboardConfig.
func (in *DashboardConfig) DeepCopy() *DashboardConfig {
	if in == nil {
		return nil
	}
	out := new(DashboardConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PanelConfig) DeepCopyInto(out *PanelConfig) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PanelConfig.
func (in *PanelConfig) DeepCopy() *PanelConfig {
	if in == nil {
		return nil
	}
	out := new(PanelConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Variable) DeepCopyInto(out *Variable) {
	*out = *in
	if in.Values != nil {
		in, out := &in.Values, &out.Values
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Variable.
func (in *Variable) DeepCopy() *Variable {
	if in == nil {
		return nil
	}
	out := new(Variable)
	in.DeepCopyInto(out)
	return out
}
