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

package broker

import (
	"maps"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// ConsumerPrefix is the prefix expected for consumer clusters.
	ConsumerPrefix = "consumer"
	// ProviderPrefix is the prefix expected for provider clusters.
	ProviderPrefix = "provider"
	// CoordinationPrefix is the prefix expected for coordination
	// clusters.
	CoordinationPrefix = "coordination"
)

// ParseKinds is a convenience wrapper around ParseKind.
func ParseKinds(kinds []string) []schema.GroupVersionKind {
	gvks := make([]schema.GroupVersionKind, len(kinds))
	for i, kind := range kinds {
		gvks[i] = ParseKind(kind)
	}
	return gvks
}

// ParseKind is a wrapper around schema.ParseKindArg that can handle core resources.
func ParseKind(kind string) schema.GroupVersionKind {
	// ConfigMap.v1.core
	if strings.HasSuffix(kind, ".core") {
		// ConfigMap, v1, core
		split := strings.SplitN(kind, ".", 3)
		// GVK{Group: "", Version: "v1", Kind: "ConfigMap"}
		return schema.GroupVersionKind{Group: "", Version: split[1], Kind: split[0]}
	}
	// Certificate.v1alpha1.example.platform-mesh.io
	// GVK{Group: "example.platform-mesh.io", Version: "v1alpha1", Kind: "Certificate"}
	gvk, _ := schema.ParseKindArg(kind)
	return *gvk
}

// SplitGroupsCore splits input into groups and core resources.
// E.g. the input "example.platform-mesh.io", "secrets.core"
// yields: []string{"example.platform-mesh.io"}, []string{"secrets"}
func SplitGroupsCore(inputs []string) ([]string, []string) {
	groups := []string{}
	core := []string{}

	for _, input := range inputs {
		if strings.HasSuffix(input, ".core") {
			// secrets.core => secrets
			core = append(core, strings.TrimSuffix(input, ".core"))
			continue
		}
		groups = append(groups, input)
	}

	return groups, core
}

// FilterAPIResources filters APIResources as returned from a discovery
// client and returns GVKs matching the passed groups and core
// resources.
func FilterAPIResources(apiResourceLists []*metav1.APIResourceList, groups, coreResources []string) []schema.GroupVersionKind {
	gvks := map[schema.GroupVersionKind]bool{}

	acceptAPIResource := func(apiResource metav1.APIResource) bool {
		if apiResource.Group == "" && apiResource.Version == "" {
			return slices.Contains(coreResources, apiResource.Name)
		}
		if strings.Contains(apiResource.Name, "/") {
			// skip subresources
			return false
		}
		if slices.Contains(groups, apiResource.Group) {
			return true
		}
		return false
	}

	for _, apiResourceList := range apiResourceLists {
		for _, apiResource := range apiResourceList.APIResources {
			if !acceptAPIResource(apiResource) {
				continue
			}
			gvk := schema.GroupVersionKind{
				Group:   apiResource.Group,
				Version: apiResource.Version,
				Kind:    apiResource.Kind,
			}
			if gvk.Version == "" {
				gvk.Version = "v1"
			}
			gvks[gvk] = true
		}
	}

	return slices.Collect(maps.Keys(gvks))
}
