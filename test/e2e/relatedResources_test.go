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
	"k8s.io/client-go/kubernetes/scheme"

	"sigs.k8s.io/controller-runtime/pkg/cluster"

	"sigs.k8s.io/multicluster-runtime/providers/single"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
	examplev1alpha1 "github.com/platform-mesh/resource-broker/api/example/v1alpha1"
	"github.com/platform-mesh/resource-broker/cmd/manager"
	"github.com/platform-mesh/resource-broker/test/utils"
)

// TestRelatedResources tests that related resources are copied from
// target to source cluster.
func TestRelatedResources(t *testing.T) {
	t.Parallel()

	// For the VM kind to be available
	require.NoError(t, examplev1alpha1.AddToScheme(scheme.Scheme))

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
				Name:      "accept-vms",
				Namespace: "default",
			},
			Spec: brokerv1alpha1.AcceptAPISpec{
				GVR: metav1.GroupVersionResource{
					Group:    "example.platform-mesh.io",
					Version:  "v1alpha1",
					Resource: "vms",
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
				Group:   "example.platform-mesh.io",
				Version: "v1alpha1",
				Kind:    "VM",
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
	vmName := "test-vm"

	t.Log("Create VM in consumer cluster")
	err = consumerCl.GetClient().Create(
		t.Context(),
		&examplev1alpha1.VM{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmName,
				Namespace: namespace,
			},
			Spec: examplev1alpha1.VMSpec{
				Arch:   "x86_64",
				Memory: 512,
			},
		},
	)
	require.NoError(t, err)

	t.Log("Wait for VM to appear in provider cluster")
	vm := &examplev1alpha1.VM{}
	require.Eventually(t, func() bool {
		err := providerCl.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      vmName,
				Namespace: namespace,
			},
			vm,
		)
		if err != nil {
			t.Logf("error getting VM from provider cluster: %v", err)
			return false
		}
		return vm.Name == vmName
	}, wait.ForeverTestTimeout, time.Second)

	t.Log("Create related ConfigMap in provider cluster")
	cmName := "related-configmap"
	cmKey := "related-key"
	cmValue := "related-value"
	err = providerCl.GetClient().Create(
		t.Context(),
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: namespace,
			},
			Data: map[string]string{
				cmKey: cmValue,
			},
		},
	)
	require.NoError(t, err)

	t.Log("Update RelatedResources in provider cluster")
	vm.Status.RelatedResources = brokerv1alpha1.RelatedResources{
		{
			Namespace: namespace,
			Name:      cmName,
			GVK: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "ConfigMap",
			},
		},
	}
	require.NoError(t, providerCl.GetClient().Status().Update(
		t.Context(),
		vm,
	))

	t.Log("Wait for RelatedResource ConfigMap to appear in source cluster")
	require.Eventually(t, func() bool {
		cm := &corev1.ConfigMap{}
		err := consumerCl.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      cmName,
				Namespace: namespace,
			},
			cm,
		)
		if err != nil {
			t.Logf("error getting related configmap from source cluster: %v", err)
			return false
		}
		return cm.Data[cmKey] == cmValue
	}, wait.ForeverTestTimeout, time.Second)
}
