// Copyright The Platform Mesh Authors.
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:rbac:groups=operator.broker.platform-mesh.io,resources=brokers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.broker.platform-mesh.io,resources=brokers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.broker.platform-mesh.io,resources=brokers/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// BrokerSpec defines the desired state of Broker.
type BrokerSpec struct {

	// TODO(ntnn): This is very generic as I'd like to use the same
	// resource for non-kcp and kcp broker, though they have very
	// different flags. Something for the future.
	// Also would be nice to use the kubeconfig provider in mcr to read
	// kubeconfig from secrets instead of mounting - but that is a whole
	// other issue.

	// Replicas determines the desired number of replicas.
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Image is the container image for the broker deployment.
	// +optional
	Image ImageSpec `json:"image,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// Resources describes the compute resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// SecurityContext holds pod-level security attributes and common container settings.
	// +optional
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`

	// Annotations are the annotations that will be attached to the Deployment.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// Labels are the labels that will be attached to the Deployment.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// ExtraArgs is a list of additional arguments to pass to
	// resource-broker.
	// +optional
	ExtraArgs []string `json:"extraArgs,omitempty"`

	// HostAliases is a list of hosts and IPs that will be injected into the pod's
	// hosts file. This is used to resolve hostnames from within the pod.
	// +optional
	HostAliases []corev1.HostAlias `json:"hostAliases,omitempty"`

	// Env is a list of environment variables to set in the container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Volumes is a list of volumes that can be mounted by containers in the pod.
	// +optional
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// VolumeMounts is a list of mounts to be added to the broker container.
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
}

// ImageSpec defines the image settings used in the deployment.
type ImageSpec struct {
	// +optional
	Repository string `json:"repository,omitempty"`
	// +optional
	Tag string `json:"tag,omitempty"`
	// +optional
	PullPolicy corev1.PullPolicy `json:"pullPolicy,omitempty"`
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

// BrokerStatus defines the observed state of Broker.
type BrokerStatus struct {
	// conditions represent the current state of the Broker resource.
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
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Broker is the Schema for the brokers API.
type Broker struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Broker
	// +required
	Spec BrokerSpec `json:"spec"`

	// status defines the observed state of Broker
	// +optional
	Status BrokerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// BrokerList contains a list of Broker.
type BrokerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Broker `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Broker{}, &BrokerList{})
}
