// Copyright 2025.
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// RelatedResource defines references to related resources.
// It should be embedded in the status of resources that should be
// synchronized by the generic reconciler.
type RelatedResource struct {
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// +kubebuilder:validation:Required
	GVK metav1.GroupVersionKind `json:"gvk"`
}

// SchemaGVK returns the schema.GroupVersionKind of the GVK.
func (rr RelatedResource) SchemaGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   rr.GVK.Group,
		Version: rr.GVK.Version,
		Kind:    rr.GVK.Kind,
	}
}

// RelatedResources is a list of RelatedResource objects.
type RelatedResources []RelatedResource
