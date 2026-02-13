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
	"maps"
)

// MergeMaps takes an arbitrary number of string:string maps and returns their
// union (in order, i.e. a later map can overwrite an existing value). This
// function always returns a map, never nil.
func MergeMaps(stringMaps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, stringMap := range stringMaps {
		maps.Copy(out, stringMap)
	}

	return out
}
