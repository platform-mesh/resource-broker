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

package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestSetAnnotation(t *testing.T) {
	t.Parallel()

	t.Run("nil annotations", func(t *testing.T) {
		t.Parallel()

		obj := &unstructured.Unstructured{}
		SetAnnotation(obj, "test-key", "test-value")

		anns := obj.GetAnnotations()
		require.NotNil(t, anns)
		assert.Equal(t, "test-value", anns["test-key"])
	})

	t.Run("existing annotations", func(t *testing.T) {
		t.Parallel()

		obj := &unstructured.Unstructured{}
		obj.SetAnnotations(map[string]string{
			"existing-key": "existing-value",
		})

		SetAnnotation(obj, "test-key", "test-value")

		anns := obj.GetAnnotations()
		assert.Equal(t, "test-value", anns["test-key"])
		assert.Equal(t, "existing-value", anns["existing-key"])
	})

	t.Run("overwrite existing annotation", func(t *testing.T) {
		t.Parallel()

		obj := &unstructured.Unstructured{}
		obj.SetAnnotations(map[string]string{
			"test-key": "old-value",
		})

		SetAnnotation(obj, "test-key", "new-value")

		anns := obj.GetAnnotations()
		assert.Equal(t, "new-value", anns["test-key"])
	})
}
