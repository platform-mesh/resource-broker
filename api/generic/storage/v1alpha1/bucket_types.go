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

// +kubebuilder:rbac:groups=storage.generic.platform-mesh.io,resources=objects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=storage.generic.platform-mesh.io,resources=objects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.generic.platform-mesh.io,resources=objects/finalizers,verbs=update

// ObjectSpec defines the desired state of Object.
type ObjectSpec struct {
	// region is the geographic region where the object storage should be created.
	// +optional
	Region string `json:"region,omitempty"`

	// versioning enables object versioning.
	// +optional
	Versioning bool `json:"versioning,omitempty"`
}

// ObjectStatus defines the observed state of Object.
type ObjectStatus struct {
	// conditions represent the current state of the Object resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedResources lists resources related to this Object.
	// +optional
	RelatedResources brokerv1alpha1.RelatedResources `json:"relatedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Object is the Schema for the objects API.
type Object struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Object
	// +required
	Spec ObjectSpec `json:"spec"`

	// status defines the observed state of Object
	// +optional
	Status ObjectStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ObjectList contains a list of Object.
type ObjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Object `json:"items"`
}

// +kubebuilder:rbac:groups=storage.generic.platform-mesh.io,resources=blocks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=storage.generic.platform-mesh.io,resources=blocks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.generic.platform-mesh.io,resources=blocks/finalizers,verbs=update

// BlockSpec defines the desired state of Block.
type BlockSpec struct {
	// size is the size of the block storage in GiB.
	// +required
	Size int64 `json:"size"`

	// storageClass is the performance tier (e.g., standard, premium, ultra).
	// +optional
	StorageClass string `json:"storageClass,omitempty"`
}

// BlockStatus defines the observed state of Block.
type BlockStatus struct {
	// conditions represent the current state of the Block resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedResources lists resources related to this Block.
	// +optional
	RelatedResources brokerv1alpha1.RelatedResources `json:"relatedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Block is the Schema for the blocks API.
type Block struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Block
	// +required
	Spec BlockSpec `json:"spec"`

	// status defines the observed state of Block
	// +optional
	Status BlockStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// BlockList contains a list of Block.
type BlockList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Block `json:"items"`
}

// +kubebuilder:rbac:groups=storage.generic.platform-mesh.io,resources=files,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=storage.generic.platform-mesh.io,resources=files/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.generic.platform-mesh.io,resources=files/finalizers,verbs=update

// FileSpec defines the desired state of File.
type FileSpec struct {
	// size is the size of the file storage in GiB.
	// +required
	Size int64 `json:"size"`

	// protocol is the file sharing protocol (e.g., nfs, smb, cifs).
	// +optional
	Protocol string `json:"protocol,omitempty"`
}

// FileStatus defines the observed state of File.
type FileStatus struct {
	// conditions represent the current state of the File resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedResources lists resources related to this File.
	// +optional
	RelatedResources brokerv1alpha1.RelatedResources `json:"relatedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// File is the Schema for the files API.
type File struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of File
	// +required
	Spec FileSpec `json:"spec"`

	// status defines the observed state of File
	// +optional
	Status FileStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// FileList contains a list of File.
type FileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []File `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Object{}, &ObjectList{})
	SchemeBuilder.Register(&Block{}, &BlockList{})
	SchemeBuilder.Register(&File{}, &FileList{})
}
