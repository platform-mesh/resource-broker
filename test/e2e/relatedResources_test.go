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

	t.Log("Create VM in source cluster")
	err = sourceCl.GetClient().Create(
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

	t.Log("Wait for VM to appear in target cluster")
	vm := &examplev1alpha1.VM{}
	require.Eventually(t, func() bool {
		err := targetCl.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      vmName,
				Namespace: namespace,
			},
			vm,
		)
		if err != nil {
			t.Logf("error getting VM from target cluster: %v", err)
			return false
		}
		return vm.Name == vmName
	}, wait.ForeverTestTimeout, time.Second)

	t.Log("Create related ConfigMap in target cluster")
	cmName := "related-configmap"
	cmKey := "related-key"
	cmValue := "related-value"
	err = targetCl.GetClient().Create(
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

	t.Log("Update RelatedResources in target cluster")
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
	require.NoError(t, targetCl.GetClient().Status().Update(
		t.Context(),
		vm,
	))

	t.Log("Wait for RelatedResource ConfigMap to appear in source cluster")
	require.Eventually(t, func() bool {
		cm := &corev1.ConfigMap{}
		err := sourceCl.GetClient().Get(
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
