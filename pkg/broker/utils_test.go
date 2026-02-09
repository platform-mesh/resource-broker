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
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestSplitGroupsCore(t *testing.T) {
	t.Run("only core resources", func(t *testing.T) {
		input := []string{"secrets.core", "configmaps.core"}
		groups, core := SplitGroupsCore(input)

		assert.Empty(t, groups)
		assert.Equal(t, []string{"secrets", "configmaps"}, core)
	})

	t.Run("only regular groups", func(t *testing.T) {
		input := []string{"example.platform-mesh.io", "custom.group.io"}
		groups, core := SplitGroupsCore(input)

		assert.Equal(t, []string{"example.platform-mesh.io", "custom.group.io"}, groups)
		assert.Empty(t, core)
	})

	t.Run("mixed core and regular groups", func(t *testing.T) {
		input := []string{"example.platform-mesh.io", "secrets.core", "custom.group.io"}
		groups, core := SplitGroupsCore(input)

		assert.Equal(t, []string{"example.platform-mesh.io", "custom.group.io"}, groups)
		assert.Equal(t, []string{"secrets"}, core)
	})

	t.Run("empty input", func(t *testing.T) {
		input := []string{}
		groups, core := SplitGroupsCore(input)

		assert.Empty(t, groups)
		assert.Empty(t, core)
	})

	t.Run("nil input", func(t *testing.T) {
		groups, core := SplitGroupsCore(nil)

		assert.Empty(t, groups)
		assert.Empty(t, core)
	})
}

func TestParseKind(t *testing.T) {
	t.Run("core resource", func(t *testing.T) {
		gvk := ParseKind("ConfigMap.v1.core")

		assert.Equal(t, "", gvk.Group)
		assert.Equal(t, "v1", gvk.Version)
		assert.Equal(t, "ConfigMap", gvk.Kind)
	})

	t.Run("custom resource", func(t *testing.T) {
		gvk := ParseKind("Certificate.v1alpha1.example.platform-mesh.io")

		assert.Equal(t, "example.platform-mesh.io", gvk.Group)
		assert.Equal(t, "v1alpha1", gvk.Version)
		assert.Equal(t, "Certificate", gvk.Kind)
	})

	t.Run("standard resource with dots in group", func(t *testing.T) {
		gvk := ParseKind("Deployment.v1.apps")

		assert.Equal(t, "apps", gvk.Group)
		assert.Equal(t, "v1", gvk.Version)
		assert.Equal(t, "Deployment", gvk.Kind)
	})
}

func TestParseKinds(t *testing.T) {
	t.Run("multiple kinds", func(t *testing.T) {
		kinds := []string{
			"ConfigMap.v1.core",
			"Certificate.v1alpha1.example.platform-mesh.io",
			"Deployment.v1.apps",
		}

		gvks := ParseKinds(kinds)

		assert.Len(t, gvks, 3)
		assert.Equal(t, "", gvks[0].Group)
		assert.Equal(t, "ConfigMap", gvks[0].Kind)
		assert.Equal(t, "example.platform-mesh.io", gvks[1].Group)
		assert.Equal(t, "Certificate", gvks[1].Kind)
		assert.Equal(t, "apps", gvks[2].Group)
		assert.Equal(t, "Deployment", gvks[2].Kind)
	})

	t.Run("empty slice", func(t *testing.T) {
		kinds := []string{}
		gvks := ParseKinds(kinds)

		assert.Empty(t, gvks)
	})

	t.Run("nil slice", func(t *testing.T) {
		gvks := ParseKinds(nil)

		assert.Empty(t, gvks)
	})
}

func TestFilterAPIResources(t *testing.T) {
	t.Run("filters by groups", func(t *testing.T) {
		apiResourceLists := []*metav1.APIResourceList{
			{
				GroupVersion: "example.platform-mesh.io/v1alpha1",
				APIResources: []metav1.APIResource{
					{
						Name:    "certificates",
						Kind:    "Certificate",
						Group:   "example.platform-mesh.io",
						Version: "v1alpha1",
					},
					{
						Name:    "issuers",
						Kind:    "Issuer",
						Group:   "example.platform-mesh.io",
						Version: "v1alpha1",
					},
				},
			},
			{
				GroupVersion: "apps/v1",
				APIResources: []metav1.APIResource{
					{
						Name:    "deployments",
						Kind:    "Deployment",
						Group:   "apps",
						Version: "v1",
					},
				},
			},
		}

		groups := []string{"example.platform-mesh.io"}
		coreResources := []string{}

		gvks := FilterAPIResources(apiResourceLists, groups, coreResources)

		assert.Len(t, gvks, 2)
		assert.Contains(t, gvks, schema.GroupVersionKind{
			Group:   "example.platform-mesh.io",
			Version: "v1alpha1",
			Kind:    "Certificate",
		})
		assert.Contains(t, gvks, schema.GroupVersionKind{
			Group:   "example.platform-mesh.io",
			Version: "v1alpha1",
			Kind:    "Issuer",
		})
	})

	t.Run("filters by core resources", func(t *testing.T) {
		apiResourceLists := []*metav1.APIResourceList{
			{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{
					{
						Name: "configmaps",
						Kind: "ConfigMap",
					},
					{
						Name: "secrets",
						Kind: "Secret",
					},
					{
						Name: "pods",
						Kind: "Pod",
					},
				},
			},
		}

		groups := []string{}
		coreResources := []string{"configmaps", "secrets"}

		gvks := FilterAPIResources(apiResourceLists, groups, coreResources)

		assert.Len(t, gvks, 2)
		assert.Contains(t, gvks, schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "ConfigMap",
		})
		assert.Contains(t, gvks, schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Secret",
		})
	})

	t.Run("skips subresources", func(t *testing.T) {
		apiResourceLists := []*metav1.APIResourceList{
			{
				GroupVersion: "apps/v1",
				APIResources: []metav1.APIResource{
					{
						Name:    "deployments",
						Kind:    "Deployment",
						Group:   "apps",
						Version: "v1",
					},
					{
						Name:    "deployments/status",
						Kind:    "Deployment",
						Group:   "apps",
						Version: "v1",
					},
					{
						Name:    "deployments/scale",
						Kind:    "Scale",
						Group:   "apps",
						Version: "v1",
					},
				},
			},
		}

		groups := []string{"apps"}
		coreResources := []string{}

		gvks := FilterAPIResources(apiResourceLists, groups, coreResources)

		assert.Len(t, gvks, 1)
		assert.Contains(t, gvks, schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1",
			Kind:    "Deployment",
		})
	})

	t.Run("handles mixed groups and core resources", func(t *testing.T) {
		apiResourceLists := []*metav1.APIResourceList{
			{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{
					{
						Name: "configmaps",
						Kind: "ConfigMap",
					},
				},
			},
			{
				GroupVersion: "example.platform-mesh.io/v1alpha1",
				APIResources: []metav1.APIResource{
					{
						Name:    "certificates",
						Kind:    "Certificate",
						Group:   "example.platform-mesh.io",
						Version: "v1alpha1",
					},
				},
			},
		}

		groups := []string{"example.platform-mesh.io"}
		coreResources := []string{"configmaps"}

		gvks := FilterAPIResources(apiResourceLists, groups, coreResources)

		assert.Len(t, gvks, 2)
		assert.Contains(t, gvks, schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "ConfigMap",
		})
		assert.Contains(t, gvks, schema.GroupVersionKind{
			Group:   "example.platform-mesh.io",
			Version: "v1alpha1",
			Kind:    "Certificate",
		})
	})

	t.Run("empty input", func(t *testing.T) {
		apiResourceLists := []*metav1.APIResourceList{}
		groups := []string{}
		coreResources := []string{}

		gvks := FilterAPIResources(apiResourceLists, groups, coreResources)

		assert.Empty(t, gvks)
	})

	t.Run("no matching resources", func(t *testing.T) {
		apiResourceLists := []*metav1.APIResourceList{
			{
				GroupVersion: "apps/v1",
				APIResources: []metav1.APIResource{
					{
						Name:    "deployments",
						Kind:    "Deployment",
						Group:   "apps",
						Version: "v1",
					},
				},
			},
		}

		groups := []string{"example.platform-mesh.io"}
		coreResources := []string{}

		gvks := FilterAPIResources(apiResourceLists, groups, coreResources)

		assert.Empty(t, gvks)
	})
}
