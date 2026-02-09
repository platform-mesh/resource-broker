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

package conditions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestUpdateConditions(t *testing.T) {
	t.Parallel()

	now := metav1.Now()

	t.Run("adds new condition", func(t *testing.T) {
		t.Parallel()

		conditions := &[]metav1.Condition{}
		newConditions := []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "Success",
				Message:            "Ready",
				LastTransitionTime: now,
			},
		}

		updated := UpdateConditions(conditions, newConditions)

		assert.True(t, updated)
		require.Len(t, *conditions, 1)
		assert.Equal(t, "Ready", (*conditions)[0].Type)
		assert.Equal(t, metav1.ConditionTrue, (*conditions)[0].Status)
	})

	t.Run("updates existing condition with status change", func(t *testing.T) {
		t.Parallel()

		oldTime := metav1.NewTime(time.Now().Add(-1 * time.Hour))
		conditions := &[]metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "Pending",
				Message:            "Not ready",
				LastTransitionTime: oldTime,
			},
		}
		newConditions := []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "Success",
				Message:            "Ready",
				LastTransitionTime: now,
			},
		}

		updated := UpdateConditions(conditions, newConditions)

		assert.True(t, updated)
		require.Len(t, *conditions, 1)
		assert.Equal(t, "Ready", (*conditions)[0].Type)
		assert.Equal(t, metav1.ConditionTrue, (*conditions)[0].Status)
		assert.Equal(t, "Success", (*conditions)[0].Reason)
		assert.Equal(t, "Ready", (*conditions)[0].Message)
	})

	t.Run("no update when condition is unchanged", func(t *testing.T) {
		t.Parallel()

		conditions := &[]metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "Success",
				Message:            "Ready",
				LastTransitionTime: now,
			},
		}
		newConditions := []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "Success",
				Message:            "Ready",
				LastTransitionTime: now,
			},
		}

		updated := UpdateConditions(conditions, newConditions)

		assert.False(t, updated)
		require.Len(t, *conditions, 1)
	})

	t.Run("updates multiple conditions", func(t *testing.T) {
		t.Parallel()

		conditions := &[]metav1.Condition{}
		newConditions := []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "Success",
				Message:            "Ready",
				LastTransitionTime: now,
			},
			{
				Type:               "Synced",
				Status:             metav1.ConditionTrue,
				Reason:             "Success",
				Message:            "Synced",
				LastTransitionTime: now,
			},
		}

		updated := UpdateConditions(conditions, newConditions)

		assert.True(t, updated)
		require.Len(t, *conditions, 2)
	})

	t.Run("updates some conditions", func(t *testing.T) {
		t.Parallel()

		oldTime := metav1.NewTime(time.Now().Add(-1 * time.Hour))
		conditions := &[]metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "Success",
				Message:            "Ready",
				LastTransitionTime: oldTime,
			},
			{
				Type:               "Synced",
				Status:             metav1.ConditionFalse,
				Reason:             "Pending",
				Message:            "Not synced",
				LastTransitionTime: oldTime,
			},
		}
		newConditions := []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "Success",
				Message:            "Ready",
				LastTransitionTime: now,
			},
			{
				Type:               "Synced",
				Status:             metav1.ConditionTrue,
				Reason:             "Success",
				Message:            "Synced",
				LastTransitionTime: now,
			},
		}

		updated := UpdateConditions(conditions, newConditions)

		assert.True(t, updated)
		require.Len(t, *conditions, 2)
		// Ready should not have been updated (same status)
		assert.Equal(t, oldTime, (*conditions)[0].LastTransitionTime)
		// Synced should have been updated (status changed)
		assert.Equal(t, metav1.ConditionTrue, (*conditions)[1].Status)
	})

	t.Run("empty new conditions", func(t *testing.T) {
		t.Parallel()

		conditions := &[]metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "Success",
				Message:            "Ready",
				LastTransitionTime: now,
			},
		}
		newConditions := []metav1.Condition{}

		updated := UpdateConditions(conditions, newConditions)

		assert.False(t, updated)
		require.Len(t, *conditions, 1)
	})

	t.Run("nil new conditions", func(t *testing.T) {
		t.Parallel()

		conditions := &[]metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "Success",
				Message:            "Ready",
				LastTransitionTime: now,
			},
		}

		updated := UpdateConditions(conditions, nil)

		assert.False(t, updated)
		require.Len(t, *conditions, 1)
	})
}
