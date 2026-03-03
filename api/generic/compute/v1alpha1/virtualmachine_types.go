/*
Copyright The Platform Mesh Authors.
SPDX-License-Identifier: Apache-2.0

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
)

// +kubebuilder:rbac:groups=compute.generic.platform-mesh.io,resources=vms,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=compute.generic.platform-mesh.io,resources=vms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=compute.generic.platform-mesh.io,resources=vms/finalizers,verbs=update

// VirtualMachineSpec defines the desired state of VirtualMachine.
type VirtualMachineSpec struct {
	// The architecture of the VirtualMachine.
	// +optional
	// +kubebuilder:validation:Enum=x86_64;arm64
	Arch string `json:"arch,omitempty"`

	// The amount of memory (in MiB) allocated to the VirtualMachine.
	// +optional
	// +kubebuilder:validation:Minimum=128
	Memory int `json:"memory,omitempty"`
}

// VirtualMachineStatus defines the observed state of VirtualMachine.
type VirtualMachineStatus struct {
	// conditions represent the current state of the VirtualMachine resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedResources lists resources related to this VM.
	// +optional
	RelatedResources brokerv1alpha1.RelatedResources `json:"relatedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VirtualMachine is the Schema for the vms API.
type VirtualMachine struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of VirtualMachine
	// +required
	Spec VirtualMachineSpec `json:"spec"`

	// status defines the observed state of VirtualMachine
	// +optional
	Status VirtualMachineStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// VirtualMachineList contains a list of VirtualMachine.
type VirtualMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VirtualMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VirtualMachine{}, &VirtualMachineList{})
}
