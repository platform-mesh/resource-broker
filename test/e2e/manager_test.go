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

	t.Log("Start a consumer and provider control plane")
	_, consumerCfg := utils.DefaultEnvTest(t)
	consumerCl, err := cluster.New(consumerCfg)
	require.NoError(t, err)
	go func() {
		err := consumerCl.Start(t.Context())
		require.NoError(t, err)
	}()

	_, providerCfg := utils.DefaultEnvTest(t)
	providerCl, err := cluster.New(providerCfg)
	require.NoError(t, err)
	go func() {
		err := providerCl.Start(t.Context())
		require.NoError(t, err)
	}()

	t.Log("Create AcceptAPI in provider control plane")
	err = providerCl.GetClient().Create(
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

	consumerCluster := single.New("consumer", consumerCl)
	providerCluster := single.New("provider", providerCl)

	mgr, err := manager.Setup(manager.Options{
		Name:     t.Name(),
		Local:    consumerCfg,
		Compute:  consumerCfg,
		Consumer: consumerCluster,
		Provider: providerCluster,
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
	err = consumerCl.GetClient().Create(
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

	t.Log("Wait for ConfigMap to appear in provider cluster")
	require.Eventually(t, func() bool {
		cm := &corev1.ConfigMap{}
		err := providerCl.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      cmName,
				Namespace: namespace,
			},
			cm,
		)
		if err != nil {
			t.Logf("error getting configmap from provider cluster: %v", err)
			return false
		}
		return cm.Data["key"] == "value"
	}, wait.ForeverTestTimeout, time.Second)
}
