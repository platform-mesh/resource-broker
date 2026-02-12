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

// ConditionType is a discrete, strongly-typed alias for metav1.Condition.Type
// values used within this package. Defining a concrete type and named
// constants improves discoverability and prevents mistyped strings.
type ConditionType string

func (c ConditionType) String() string { return string(c) }

const (
	// ConditionResourceCopied indicates the resource was copied/created/updated
	// on the destination cluster.
	ConditionResourceCopied ConditionType = "Copied"

	// ConditionStatusSynced indicates the status was copied back to the
	// source cluster.
	ConditionStatusSynced ConditionType = "StatusSynced"
)
