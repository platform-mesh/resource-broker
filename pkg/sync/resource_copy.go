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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// makeCond constructs a metav1.Condition. The status parameter is a boolean
// where true maps to metav1.ConditionTrue and false to metav1.ConditionFalse.
func makeCond(t ConditionType, ok bool, reason, msg string) metav1.Condition {
	s := metav1.ConditionFalse
	if ok {
		s = metav1.ConditionTrue
	}
	return metav1.Condition{
		Type:    t.String(),
		Status:  s,
		Reason:  reason,
		Message: msg,
	}
}

// StripClusterMetadata returns a deep copy of the provided Unstructured with
// cluster-specific fields removed so the object is safe to write in another
// cluster.
func StripClusterMetadata(obj *unstructured.Unstructured) *unstructured.Unstructured {
	c := obj.DeepCopy()
	delete(c.Object, "status")
	if m, ok := c.Object["metadata"].(map[string]any); ok {
		delete(m, "resourceVersion")
		delete(m, "uid")
		delete(m, "creationTimestamp")
		delete(m, "managedFields")
		delete(m, "generation")
		delete(m, "ownerReferences")
		delete(m, "finalizers")
		delete(m, "annotations")
		delete(m, "labels")
	}
	return c
}

// EqualObjects returns true if the two unstructured objects are
// equal after removing cluster-specific metadata and status.
func EqualObjects(a, b *unstructured.Unstructured) bool {
	return cmp.Equal(
		StripClusterMetadata(a).Object,
		StripClusterMetadata(b).Object,
		cmpopts.EquateEmpty(),
	)
}

// CopyResource copies a resource from source to target and reflects the
// status back.
func CopyResource(
	ctx context.Context,
	gvk schema.GroupVersionKind,
	name types.NamespacedName,
	source, target client.Client,
) (metav1.Condition, error) {
	sourceObj := &unstructured.Unstructured{}
	sourceObj.SetGroupVersionKind(gvk)

	if err := source.Get(ctx, name, sourceObj); err != nil {
		return makeCond(ConditionResourceCopied, false, "GetSourceFailed", err.Error()), err
	}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(gvk)
	if err := target.Get(ctx, name, existing); err != nil {
		if errors.IsNotFound(err) {
			targetObj := StripClusterMetadata(sourceObj)
			if err := target.Create(ctx, targetObj); err != nil {
				return makeCond(ConditionResourceCopied, false, "CreateFailed", err.Error()), err
			}
			return makeCond(ConditionResourceCopied, true, "Created", "Resource created on destination"), nil
		}
		return makeCond(ConditionResourceCopied, false, "GetTargetFailed", err.Error()), err
	}

	if !EqualObjects(sourceObj, existing) {
		// TODO this only copies fields from source to target, omitting
		// if source deletes a field that exists in target.
		// A more robust merge strategy would be better.
		toUpdate := existing.DeepCopy()
		for k, v := range sourceObj.Object {
			if k == "metadata" || k == "status" {
				continue
			}
			toUpdate.Object[k] = v
		}
		if err := target.Update(ctx, toUpdate); err != nil {
			return makeCond(ConditionResourceCopied, false, "UpdateFailed", err.Error()), err
		}
		return makeCond(ConditionResourceCopied, true, "Updated", "Resource updated on destination"), nil
	}

	if status, ok := existing.Object["status"]; ok {
		sourceObj.Object["status"] = status
		if err := source.Status().Update(ctx, sourceObj); err != nil {
			return makeCond(ConditionStatusSynced, false, "StatusUpdateFailed", err.Error()), err
		}
	}

	return makeCond(ConditionStatusSynced, true, "StatusSynced", "Status copied back to source"), nil
}
