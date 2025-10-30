/*
Copyright 2025.
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
	"fmt"
	"slices"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// AcceptAPISpec defines the desired state of AcceptAPI.
type AcceptAPISpec struct {
	// GVR is the GroupVersionResource of the API to accept.
	// +kubebuilder:validation:Required
	GVR metav1.GroupVersionResource `json:"gvr"`

	// Filters to select which resources of the GVR to accept.
	Filters []Filter `json:"filters,omitempty"`

	// // Template is the template to use for the accepted resources.
	// Template metav1.RawExtension `json:"template,omitempty"`
}

// Filter defines a filter to select resources.
type Filter struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Key      string   `json:"key"`
	ValueIn  []string `json:"valueIn,omitempty"`
	Boundary Boundary `json:"boundary,omitempty"`
}

// Boundary defines a min and max boundary for numeric filtering.
type Boundary struct {
	Min *int `json:"min,omitempty"`
	Max *int `json:"max,omitempty"`
}

// AppliesTo checks if the given object matches the filters.
func (acceptAPI *AcceptAPI) AppliesTo(gvr metav1.GroupVersionResource, obj *unstructured.Unstructured) bool {
	if acceptAPI.Spec.GVR.String() != gvr.String() {
		return false
	}

	for _, filter := range acceptAPI.Spec.Filters {
		fields := []string{"spec"}
		fields = append(fields, strings.Split(filter.Key, ".")...)

		valRaw, found, err := unstructured.NestedFieldNoCopy(obj.Object, fields...)
		if err != nil || !found {
			return false
		}

		// Convert to string for consistent comparison across different numeric and string types.
		val := fmt.Sprintf("%v", valRaw)

		// very rudimentary and most certainly not to spec, but it's
		// enough for a poc

		if len(filter.ValueIn) > 0 {
			if !slices.Contains(filter.ValueIn, val) {
				return false
			}
		}

		if filter.Boundary.Min != nil && *filter.Boundary.Min >= 0 {
			numVal, err := strconv.Atoi(val)
			if err != nil || numVal < *filter.Boundary.Min {
				return false
			}
		}

		if filter.Boundary.Max != nil && *filter.Boundary.Max >= 0 {
			numVal, err := strconv.Atoi(val)
			if err != nil || numVal > *filter.Boundary.Max {
				return false
			}
		}
	}

	return true
}

// AcceptAPIStatus defines the observed state of AcceptAPI.
type AcceptAPIStatus struct {
	// conditions represent the current state of the AcceptAPI resource.
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

// AcceptAPI is the Schema for the acceptapis API.
type AcceptAPI struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AcceptAPISpec   `json:"spec,omitempty"`
	Status AcceptAPIStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AcceptAPIList contains a list of AcceptAPI.
type AcceptAPIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AcceptAPI `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AcceptAPI{}, &AcceptAPIList{})
}
