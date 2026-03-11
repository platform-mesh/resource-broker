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

// +kubebuilder:rbac:groups=ai.generic.platform-mesh.io,resources=hostedmodels,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai.generic.platform-mesh.io,resources=hostedmodels/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai.generic.platform-mesh.io,resources=hostedmodels/finalizers,verbs=update

// HostedModelSpec defines the desired state of HostedModel.
type HostedModelSpec struct {
	// modelID is the identifier of the AI model.
	// +required
	ModelID string `json:"modelID"`

	// runtime is the inference runtime to use (e.g., vllm, triton, ollama).
	// +optional
	Runtime string `json:"runtime,omitempty"`
}

// HostedModelStatus defines the observed state of HostedModel.
type HostedModelStatus struct {
	// conditions represent the current state of the HostedModel resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedResources lists resources related to this HostedModel.
	// +optional
	RelatedResources brokerv1alpha1.RelatedResources `json:"relatedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// HostedModel is the Schema for the hostedmodels API.
type HostedModel struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of HostedModel
	// +required
	Spec HostedModelSpec `json:"spec"`

	// status defines the observed state of HostedModel
	// +optional
	Status HostedModelStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// HostedModelList contains a list of HostedModel.
type HostedModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HostedModel `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HostedModel{}, &HostedModelList{})
}
