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

// +kubebuilder:rbac:groups=networking.generic.platform-mesh.io,resources=virtualnetworks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.generic.platform-mesh.io,resources=virtualnetworks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.generic.platform-mesh.io,resources=virtualnetworks/finalizers,verbs=update

// VirtualNetworkSpec defines the desired state of VirtualNetwork.
type VirtualNetworkSpec struct {
	// cidr is the CIDR block for the network.
	// +optional
	CIDR string `json:"cidr,omitempty"`

	// region is the geographic region where the network should be created.
	// +optional
	Region string `json:"region,omitempty"`
}

// VirtualNetworkStatus defines the observed state of VirtualNetwork.
type VirtualNetworkStatus struct {
	// conditions represent the current state of the VirtualNetwork resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedResources lists resources related to this VirtualNetwork.
	// +optional
	RelatedResources brokerv1alpha1.RelatedResources `json:"relatedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VirtualNetwork is the Schema for the virtualnetworks API.
type VirtualNetwork struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of VirtualNetwork
	// +required
	Spec VirtualNetworkSpec `json:"spec"`

	// status defines the observed state of VirtualNetwork
	// +optional
	Status VirtualNetworkStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// VirtualNetworkList contains a list of VirtualNetwork.
type VirtualNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VirtualNetwork `json:"items"`
}

// +kubebuilder:rbac:groups=networking.generic.platform-mesh.io,resources=peerings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.generic.platform-mesh.io,resources=peerings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.generic.platform-mesh.io,resources=peerings/finalizers,verbs=update

// PeeringSpec defines the desired state of Peering.
type PeeringSpec struct {
	// localNetworkRef is a reference to the local virtual network.
	// +required
	LocalNetworkRef string `json:"localNetworkRef"`

	// remoteNetworkRef is a reference to the remote virtual network.
	// +required
	RemoteNetworkRef string `json:"remoteNetworkRef"`

	// allowForwardedTraffic allows forwarded traffic between the peered networks.
	// +optional
	AllowForwardedTraffic bool `json:"allowForwardedTraffic,omitempty"`

	// allowGatewayTransit allows gateway transit between the peered networks.
	// +optional
	AllowGatewayTransit bool `json:"allowGatewayTransit,omitempty"`
}

// PeeringStatus defines the observed state of Peering.
type PeeringStatus struct {
	// conditions represent the current state of the Peering resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedResources lists resources related to this Peering.
	// +optional
	RelatedResources brokerv1alpha1.RelatedResources `json:"relatedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Peering is the Schema for the peerings API.
type Peering struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Peering
	// +required
	Spec PeeringSpec `json:"spec"`

	// status defines the observed state of Peering
	// +optional
	Status PeeringStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// PeeringList contains a list of Peering.
type PeeringList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Peering `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VirtualNetwork{}, &VirtualNetworkList{})
	SchemeBuilder.Register(&Peering{}, &PeeringList{})
}
