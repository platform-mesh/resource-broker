//go:build kube_legacy

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

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
	examplev1alpha1 "github.com/platform-mesh/resource-broker/api/example/v1alpha1"
	"github.com/platform-mesh/resource-broker/test/e2e/internal/manager"
	"github.com/platform-mesh/resource-broker/pkg/broker/generic"
)

// TestRelatedResources tests that related resources are copied from
// target to source cluster.
func TestRelatedResources(t *testing.T) {
	t.Skip("pending kcp test rewrite")
	t.Parallel()

	frame := NewFrame(t)
	rules := append(
		resourceRules("example.platform-mesh.io", "vms"),
		resourceRules("", "configmaps")...,
	)
	consumer := frame.NewConsumer(t, "consumer", consumerRules(rules...))
	provider := frame.NewProvider(t, "provider", providerRules(rules...))

	t.Log("Create AcceptAPI in provider control plane")
	err := provider.Cluster.GetClient().Create(
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

	mgrOptions := frame.Options(t)
	mgrOptions.WatchKinds = []string{"VM.v1alpha1.example.platform-mesh.io"}

	mgr, err := manager.Setup(mgrOptions)
	require.NoError(t, err)

	go func() {
		err := mgr.Start(t.Context())
		assert.NoError(t, err)
	}()

	namespace := "default"
	vmName := "test-vm"

	t.Log("Create VM in consumer control plane")
	err = consumer.Cluster.GetClient().Create(
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

	t.Log("Wait for VM to appear in provider control plane")
	providerVMName := generic.SanitizeClusterName("consumer#consumer#cluster") + "-" + vmName
	vm := &examplev1alpha1.VM{}
	require.Eventually(t, func() bool {
		err := provider.Cluster.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      providerVMName,
				Namespace: namespace,
			},
			vm,
		)
		if err != nil {
			t.Logf("error getting VM from provider control plane: %v", err)
			return false
		}
		return vm.Name == providerVMName
	}, wait.ForeverTestTimeout, time.Second)

	t.Log("Create related ConfigMap in provider control plane")
	cmName := "related-configmap"
	cmKey := "related-key"
	cmValue := "related-value"
	err = provider.Cluster.GetClient().Create(
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

	t.Log("Update RelatedResources in provider control plane")
	require.Eventually(t, func() bool {
		vm := &examplev1alpha1.VM{}
		err := provider.Cluster.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      providerVMName,
				Namespace: namespace,
			},
			vm,
		)
		if err != nil {
			t.Logf("error getting VM from provider control plane: %v", err)
			return false
		}
		vm.Status.RelatedResources = brokerv1alpha1.RelatedResources{
			"configmap": brokerv1alpha1.RelatedResource{
				Namespace: namespace,
				Name:      cmName,
				GVK: metav1.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "ConfigMap",
				},
			},
		}

		err = provider.Cluster.GetClient().Status().Update(
			t.Context(),
			vm,
		)
		return err == nil
	}, wait.ForeverTestTimeout, time.Second)

	t.Log("Wait for RelatedResource ConfigMap to appear in source control plane")
	require.Eventually(t, func() bool {
		cm := &corev1.ConfigMap{}
		err := consumer.Cluster.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      cmName,
				Namespace: namespace,
			},
			cm,
		)
		if err != nil {
			t.Logf("error getting related configmap from source control plane: %v", err)
			return false
		}
		return cm.Data[cmKey] == cmValue
	}, wait.ForeverTestTimeout, time.Second)
}
