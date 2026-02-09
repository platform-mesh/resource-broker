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
	t.Parallel()

	tests := map[string]struct {
		input          []string
		expectedGroups []string
		expectedCore   []string
	}{
		"only core resources": {
			input:          []string{"secrets.core", "configmaps.core"},
			expectedGroups: []string{},
			expectedCore:   []string{"secrets", "configmaps"},
		},
		"only regular groups": {
			input:          []string{"example.platform-mesh.io", "custom.group.io"},
			expectedGroups: []string{"example.platform-mesh.io", "custom.group.io"},
			expectedCore:   []string{},
		},
		"mixed core and regular groups": {
			input:          []string{"example.platform-mesh.io", "secrets.core", "custom.group.io"},
			expectedGroups: []string{"example.platform-mesh.io", "custom.group.io"},
			expectedCore:   []string{"secrets"},
		},
		"empty input": {
			input:          []string{},
			expectedGroups: []string{},
			expectedCore:   []string{},
		},
		"nil input": {
			input:          nil,
			expectedGroups: []string{},
			expectedCore:   []string{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			groups, core := SplitGroupsCore(tc.input)
			assert.Equal(t, tc.expectedGroups, groups)
			assert.Equal(t, tc.expectedCore, core)
		})
	}
}

func TestParseKind(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input           string
		expectedGroup   string
		expectedVersion string
		expectedKind    string
	}{
		"core resource": {
			input:           "ConfigMap.v1.core",
			expectedGroup:   "",
			expectedVersion: "v1",
			expectedKind:    "ConfigMap",
		},
		"custom resource": {
			input:           "Certificate.v1alpha1.example.platform-mesh.io",
			expectedGroup:   "example.platform-mesh.io",
			expectedVersion: "v1alpha1",
			expectedKind:    "Certificate",
		},
		"standard resource with dots in group": {
			input:           "Deployment.v1.apps",
			expectedGroup:   "apps",
			expectedVersion: "v1",
			expectedKind:    "Deployment",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			gvk := ParseKind(tc.input)
			assert.Equal(t, tc.expectedGroup, gvk.Group)
			assert.Equal(t, tc.expectedVersion, gvk.Version)
			assert.Equal(t, tc.expectedKind, gvk.Kind)
		})
	}
}

func TestParseKinds(t *testing.T) {
	t.Parallel()

	t.Run("multiple kinds", func(t *testing.T) {
		t.Parallel()

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
		t.Parallel()

		kinds := []string{}
		gvks := ParseKinds(kinds)

		assert.Empty(t, gvks)
	})

	t.Run("nil slice", func(t *testing.T) {
		t.Parallel()

		gvks := ParseKinds(nil)

		assert.Empty(t, gvks)
	})
}

func TestFilterAPIResources(t *testing.T) {
	t.Parallel()

	t.Run("filters by groups", func(t *testing.T) {
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

		apiResourceLists := []*metav1.APIResourceList{
			{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{
					{
						Name: "configmaps",
						Kind: "ConfigMap",
					},
					{
						Name: "pods",
						Kind: "Pod",
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
		t.Parallel()

		apiResourceLists := []*metav1.APIResourceList{}
		groups := []string{}
		coreResources := []string{}

		gvks := FilterAPIResources(apiResourceLists, groups, coreResources)

		assert.Empty(t, gvks)
	})

	t.Run("no matching resources", func(t *testing.T) {
		t.Parallel()

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
