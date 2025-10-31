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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"sigs.k8s.io/controller-runtime/pkg/cluster"

	"sigs.k8s.io/multicluster-runtime/providers/single"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
	"github.com/platform-mesh/resource-broker/cmd/manager"
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
	go func() {
		err := sourceCl.Start(t.Context())
		require.NoError(t, err)
	}()

	_, targetCfg := utils.DefaultEnvTest(t)
	targetCl, err := cluster.New(targetCfg)
	require.NoError(t, err)
	go func() {
		err := targetCl.Start(t.Context())
		require.NoError(t, err)
	}()

	t.Log("Create AcceptAPI in target control plane")
	err = targetCl.GetClient().Create(
		t.Context(),
		&brokerv1alpha1.AcceptAPI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "accept-configmaps",
				Namespace: "default",
			},
			Spec: brokerv1alpha1.AcceptAPISpec{
				GVR: metav1.GroupVersionResource{
					Group:    "",
					Version:  "v1",
					Resource: "configmaps",
				},
			},
		},
	)
	require.NoError(t, err)

	sourceCluster := single.New("source", sourceCl)
	targetCluster := single.New("target", targetCl)

	mgr, err := manager.Setup(manager.Options{
		Name:    t.Name(),
		Local:   sourceCfg,
		Compute: sourceCfg,
		Source:  sourceCluster,
		Target:  targetCluster,
		GVKs: []schema.GroupVersionKind{
			{
				Group:   "",
				Version: "v1",
				Kind:    "ConfigMap",
			},
		},
		MgrOptions: ManagerOptions(),
	})
	require.NoError(t, err)

	go func() {
		err := mgr.Start(t.Context())
		assert.NoError(t, err)
	}()

	namespace := "default"
	cmName := "test-configmap"

	t.Log("Create ConfigMap in source cluster")
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

	t.Log("Wait for ConfigMap to appear in target cluster")
	require.Eventually(t, func() bool {
		cm := &corev1.ConfigMap{}
		err := targetCl.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      cmName,
				Namespace: namespace,
			},
			cm,
		)
		if err != nil {
			t.Logf("error getting configmap from target cluster: %v", err)
			return false
		}
		return cm.Data["key"] == "value"
	}, wait.ForeverTestTimeout, time.Second)
}
