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

// +kubebuilder:rbac:groups=compute.generic.platform-mesh.io,resources=kubernetesclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=compute.generic.platform-mesh.io,resources=kubernetesclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=compute.generic.platform-mesh.io,resources=kubernetesclusters/finalizers,verbs=update

// KubernetesClusterSpec defines the desired state of KubernetesCluster.
type KubernetesClusterSpec struct {
	// version is the desired Kubernetes version.
	// +required
	Version string `json:"version"`

	// nodeCount is the desired number of worker nodes.
	// +optional
	// +kubebuilder:validation:Minimum=1
	NodeCount *int32 `json:"nodeCount,omitempty"`
}

// KubernetesClusterStatus defines the observed state of KubernetesCluster.
type KubernetesClusterStatus struct {
	// conditions represent the current state of the KubernetesCluster resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedResources lists resources related to this KubernetesCluster.
	// +optional
	RelatedResources brokerv1alpha1.RelatedResources `json:"relatedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// KubernetesCluster is the Schema for the kubernetesclusters API.
type KubernetesCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of KubernetesCluster
	// +required
	Spec KubernetesClusterSpec `json:"spec"`

	// status defines the observed state of KubernetesCluster
	// +optional
	Status KubernetesClusterStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// KubernetesClusterList contains a list of KubernetesCluster.
type KubernetesClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubernetesCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubernetesCluster{}, &KubernetesClusterList{})
}
