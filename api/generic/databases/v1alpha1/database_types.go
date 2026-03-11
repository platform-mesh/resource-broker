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

// +kubebuilder:rbac:groups=databases.generic.platform-mesh.io,resources=sqls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=databases.generic.platform-mesh.io,resources=sqls/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=databases.generic.platform-mesh.io,resources=sqls/finalizers,verbs=update

// SQLSpec defines the desired state of SQL.
type SQLSpec struct {
	// engine is the database engine type (e.g., postgres, mysql).
	// +required
	Engine string `json:"engine"`

	// version is the database engine version.
	// +optional
	Version string `json:"version,omitempty"`
}

// SQLStatus defines the observed state of SQL.
type SQLStatus struct {
	// conditions represent the current state of the SQL resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedResources lists resources related to this SQL.
	// +optional
	RelatedResources brokerv1alpha1.RelatedResources `json:"relatedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SQL is the Schema for the sqls API.
type SQL struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of SQL
	// +required
	Spec SQLSpec `json:"spec"`

	// status defines the observed state of SQL
	// +optional
	Status SQLStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// SQLList contains a list of SQL.
type SQLList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SQL `json:"items"`
}

// +kubebuilder:rbac:groups=databases.generic.platform-mesh.io,resources=nosqls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=databases.generic.platform-mesh.io,resources=nosqls/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=databases.generic.platform-mesh.io,resources=nosqls/finalizers,verbs=update

// NoSQLSpec defines the desired state of NoSQL.
type NoSQLSpec struct {
	// engine is the database engine type (e.g., mongodb, couchdb, dynamodb).
	// +required
	Engine string `json:"engine"`

	// version is the database engine version.
	// +optional
	Version string `json:"version,omitempty"`
}

// NoSQLStatus defines the observed state of NoSQL.
type NoSQLStatus struct {
	// conditions represent the current state of the NoSQL resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedResources lists resources related to this NoSQL.
	// +optional
	RelatedResources brokerv1alpha1.RelatedResources `json:"relatedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// NoSQL is the Schema for the nosqls API.
type NoSQL struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of NoSQL
	// +required
	Spec NoSQLSpec `json:"spec"`

	// status defines the observed state of NoSQL
	// +optional
	Status NoSQLStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// NoSQLList contains a list of NoSQL.
type NoSQLList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NoSQL `json:"items"`
}

// +kubebuilder:rbac:groups=databases.generic.platform-mesh.io,resources=keyvalues,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=databases.generic.platform-mesh.io,resources=keyvalues/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=databases.generic.platform-mesh.io,resources=keyvalues/finalizers,verbs=update

// KeyValueSpec defines the desired state of KeyValue.
type KeyValueSpec struct {
	// engine is the database engine type (e.g., redis, memcached, etcd).
	// +required
	Engine string `json:"engine"`

	// version is the database engine version.
	// +optional
	Version string `json:"version,omitempty"`
}

// KeyValueStatus defines the observed state of KeyValue.
type KeyValueStatus struct {
	// conditions represent the current state of the KeyValue resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedResources lists resources related to this KeyValue.
	// +optional
	RelatedResources brokerv1alpha1.RelatedResources `json:"relatedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// KeyValue is the Schema for the keyvalues API.
type KeyValue struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of KeyValue
	// +required
	Spec KeyValueSpec `json:"spec"`

	// status defines the observed state of KeyValue
	// +optional
	Status KeyValueStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// KeyValueList contains a list of KeyValue.
type KeyValueList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KeyValue `json:"items"`
}

// +kubebuilder:rbac:groups=databases.generic.platform-mesh.io,resources=vectors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=databases.generic.platform-mesh.io,resources=vectors/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=databases.generic.platform-mesh.io,resources=vectors/finalizers,verbs=update

// VectorSpec defines the desired state of Vector.
type VectorSpec struct {
	// engine is the database engine type (e.g., pinecone, milvus, weaviate, qdrant).
	// +required
	Engine string `json:"engine"`

	// version is the database engine version.
	// +optional
	Version string `json:"version,omitempty"`
}

// VectorStatus defines the observed state of Vector.
type VectorStatus struct {
	// conditions represent the current state of the Vector resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedResources lists resources related to this Vector.
	// +optional
	RelatedResources brokerv1alpha1.RelatedResources `json:"relatedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Vector is the Schema for the vectors API.
type Vector struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Vector
	// +required
	Spec VectorSpec `json:"spec"`

	// status defines the observed state of Vector
	// +optional
	Status VectorStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// VectorList contains a list of Vector.
type VectorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Vector `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SQL{}, &SQLList{})
	SchemeBuilder.Register(&NoSQL{}, &NoSQLList{})
	SchemeBuilder.Register(&KeyValue{}, &KeyValueList{})
	SchemeBuilder.Register(&Vector{}, &VectorList{})
}
