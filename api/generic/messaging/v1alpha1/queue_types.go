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

// +kubebuilder:rbac:groups=messaging.generic.platform-mesh.io,resources=pubsubs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=messaging.generic.platform-mesh.io,resources=pubsubs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=messaging.generic.platform-mesh.io,resources=pubsubs/finalizers,verbs=update

// PubSubSpec defines the desired state of PubSub.
type PubSubSpec struct {
	// maxSize is the maximum number of messages the queue can hold.
	// +optional
	MaxSize int `json:"maxSize,omitempty"`

	// retentionSeconds is the message retention period in seconds.
	// +optional
	RetentionSeconds int `json:"retentionSeconds,omitempty"`
}

// PubSubStatus defines the observed state of PubSub.
type PubSubStatus struct {
	// conditions represent the current state of the PubSub resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedResources lists resources related to this PubSub.
	// +optional
	RelatedResources brokerv1alpha1.RelatedResources `json:"relatedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// PubSub is the Schema for the pubsubs API.
type PubSub struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of PubSub
	// +required
	Spec PubSubSpec `json:"spec"`

	// status defines the observed state of PubSub
	// +optional
	Status PubSubStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// PubSubList contains a list of PubSub.
type PubSubList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PubSub `json:"items"`
}

// +kubebuilder:rbac:groups=messaging.generic.platform-mesh.io,resources=eventstreamings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=messaging.generic.platform-mesh.io,resources=eventstreamings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=messaging.generic.platform-mesh.io,resources=eventstreamings/finalizers,verbs=update

// EventStreamingSpec defines the desired state of EventStreaming.
type EventStreamingSpec struct {
	// engine is the streaming engine type (e.g., kafka, kinesis, eventbridge).
	// +required
	Engine string `json:"engine"`

	// partitions is the number of partitions for the stream.
	// +optional
	Partitions int `json:"partitions,omitempty"`

	// retentionHours is the message retention period in hours.
	// +optional
	RetentionHours int `json:"retentionHours,omitempty"`
}

// EventStreamingStatus defines the observed state of EventStreaming.
type EventStreamingStatus struct {
	// conditions represent the current state of the EventStreaming resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedResources lists resources related to this EventStreaming.
	// +optional
	RelatedResources brokerv1alpha1.RelatedResources `json:"relatedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// EventStreaming is the Schema for the eventstreamings API.
type EventStreaming struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of EventStreaming
	// +required
	Spec EventStreamingSpec `json:"spec"`

	// status defines the observed state of EventStreaming
	// +optional
	Status EventStreamingStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// EventStreamingList contains a list of EventStreaming.
type EventStreamingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EventStreaming `json:"items"`
}

// +kubebuilder:rbac:groups=messaging.generic.platform-mesh.io,resources=messagebrokers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=messaging.generic.platform-mesh.io,resources=messagebrokers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=messaging.generic.platform-mesh.io,resources=messagebrokers/finalizers,verbs=update

// MessageBrokerSpec defines the desired state of MessageBroker.
type MessageBrokerSpec struct {
	// engine is the message broker engine type (e.g., rabbitmq, activemq, amazonsqs).
	// +required
	Engine string `json:"engine"`

	// version is the broker engine version.
	// +optional
	Version string `json:"version,omitempty"`

	// instanceType is the instance size for the broker.
	// +optional
	InstanceType string `json:"instanceType,omitempty"`
}

// MessageBrokerStatus defines the observed state of MessageBroker.
type MessageBrokerStatus struct {
	// conditions represent the current state of the MessageBroker resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedResources lists resources related to this MessageBroker.
	// +optional
	RelatedResources brokerv1alpha1.RelatedResources `json:"relatedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// MessageBroker is the Schema for the messagebrokers API.
type MessageBroker struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of MessageBroker
	// +required
	Spec MessageBrokerSpec `json:"spec"`

	// status defines the observed state of MessageBroker
	// +optional
	Status MessageBrokerStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// MessageBrokerList contains a list of MessageBroker.
type MessageBrokerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MessageBroker `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PubSub{}, &PubSubList{})
	SchemeBuilder.Register(&EventStreaming{}, &EventStreamingList{})
	SchemeBuilder.Register(&MessageBroker{}, &MessageBrokerList{})
}
