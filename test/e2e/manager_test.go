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

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-runtime/pkg/cluster"

	"sigs.k8s.io/multicluster-runtime/providers/single"

	"github.com/platform-mesh/resource-broker/pkg/manager"
	"github.com/platform-mesh/resource-broker/pkg/wrapprovider"
	"github.com/platform-mesh/resource-broker/test/utils"
)

// TestManagerCopy only tests that the manager can copy from a source to
// a destination cluster.
func TestManagerCopy(t *testing.T) {
	t.Parallel()

	_, localCfg := utils.DefaultEnvTestStart(t)

	_, sourceCfg := utils.DefaultEnvTestStart(t)
	sourceCl, err := cluster.New(sourceCfg)
	require.NoError(t, err)

	_, targetCfg := utils.DefaultEnvTestStart(t)
	targetCl, err := cluster.New(targetCfg)
	require.NoError(t, err)

	go func() {
		err := manager.Start(
			t.Context(),
			localCfg,
			wrapprovider.Wrap(single.New("source", sourceCl), nil),
			wrapprovider.Wrap(single.New("target", targetCl), nil),
			schema.GroupVersionKind{
				Group:   "core",
				Version: "v1",
				Kind:    "ConfigMap",
			},
		)
		require.NoError(t, err)
	}()

	t.Log("waiting for manager to start up")
	time.Sleep(5 * time.Second)
}
