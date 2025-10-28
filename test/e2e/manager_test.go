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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-runtime/pkg/cluster"

	"sigs.k8s.io/multicluster-runtime/providers/clusters"

	"github.com/platform-mesh/resource-broker/pkg/manager"
	"github.com/platform-mesh/resource-broker/test/utils"
)

// TestManagerCopy only tests that the manager can copy from a source to
// a destination cluster.
func TestManagerCopy(t *testing.T) {
	t.Parallel()

	t.Log("Start a source and target control plane")
	_, sourceCfg := utils.DefaultEnvTest(t)
	sourceCl, err := cluster.New(sourceCfg)
	require.NoError(t, err)

	_, targetCfg := utils.DefaultEnvTest(t)
	targetCl, err := cluster.New(targetCfg)
	require.NoError(t, err)

	sourceClusters := clusters.New()
	sourceClusters.ErrorHandler = func(err error, msg string, keysAndValues ...any) {
		t.Logf("source cluster error: %v, %s, %v", err, msg, keysAndValues)
	}

	targetClusters := clusters.New()
	targetClusters.ErrorHandler = func(err error, msg string, keysAndValues ...any) {
		t.Logf("target cluster error: %v, %s, %v", err, msg, keysAndValues)
	}

	mgr, err := manager.Setup(
		targetCfg, // Using target control plane as "local" control plane, as if the manager would run there
		sourceClusters,
		targetClusters,
		schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "ConfigMap",
		},
	)
	require.NoError(t, err)

	go func() {
		err := mgr.Start(t.Context())
		require.NoError(t, err)
	}()

	err = sourceClusters.Add(t.Context(), "source", sourceCl, mgr)
	require.NoError(t, err)
	err = targetClusters.Add(t.Context(), "target", targetCl, mgr)
	require.NoError(t, err)

	namespace := "default"
	cmName := "test-configmap"

	err = sourceCl.GetClient().Create(
		t.Context(),
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: namespace,
			},
			Data: map[string]string{
				"key": "value",
			},
		},
	)
	require.NoError(t, err)

	// require.Eventually(t, func() bool {
	// 	cm := &corev1.ConfigMap{}
	// 	err := targetCl.GetClient().Get(
	// 		t.Context(),
	// 		types.NamespacedName{
	// 			Name:      cmName,
	// 			Namespace: namespace,
	// 		},
	// 		cm,
	// 	)
	// 	if err != nil {
	// 		t.Logf("error getting configmap from target cluster: %v", err)
	// 		return false
	// 	}
	// 	return cm.Data["key"] == "value"
	// }, wait.ForeverTestTimeout, time.Second)
	time.Sleep(5 * time.Second) // TODO(ntnn): replace once implemented
}
