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

package sync

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
)

// CollectRelatedResources retrieves the related resources from the
// status of a provider resource.
func CollectRelatedResources(ctx context.Context, providerClient client.Client, gvk schema.GroupVersionKind, namespacedName types.NamespacedName) (map[string]brokerv1alpha1.RelatedResource, error) {
	providerObj := &unstructured.Unstructured{}
	providerObj.SetGroupVersionKind(gvk)
	if err := providerClient.Get(ctx, namespacedName, providerObj); err != nil {
		return nil, fmt.Errorf("failed to get resource: %w", err)
	}

	relatedResourcesI, found, err := unstructured.NestedMap(providerObj.Object, "status", "relatedResources")
	if err != nil {
		return nil, fmt.Errorf("failed to get related resources from synced resource status: %w", err)
	}
	if !found {
		return nil, nil
	}

	relatedResources := make(map[string]brokerv1alpha1.RelatedResource, len(relatedResourcesI))
	for key, rrI := range relatedResourcesI {
		rrMap, ok := rrI.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("failed to cast related resource %q from synced resource status", key)
		}

		var rr brokerv1alpha1.RelatedResource
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(rrMap, &rr); err != nil {
			return nil, fmt.Errorf("failed to convert related resource %q from synced resource status: %w", key, err)
		}

		relatedResources[key] = rr
	}

	return relatedResources, nil
}
