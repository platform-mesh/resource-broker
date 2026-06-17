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
	"github.com/platform-mesh/resource-broker/test/e2e/internal/manager"
	"github.com/platform-mesh/resource-broker/pkg/broker/generic"
)

// TestManagerCopy only tests that the manager can copy from a source to
// a destination cluster.
func TestManagerCopy(t *testing.T) {
	t.Skip("pending kcp test rewrite")
	t.Parallel()

	frame := NewFrame(t)
	cmRules := resourceRules("", "configmaps")
	provider := frame.NewProvider(t, "provider", providerRules(cmRules...))
	consumer := frame.NewConsumer(t, "consumer", consumerRules(cmRules...))

	t.Log("Create AcceptAPI in provider control plane")
	err := provider.Cluster.GetClient().Create(
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

	mgrOptions := frame.Options(t)
	mgrOptions.WatchKinds = []string{"ConfigMap.v1.core"}

	mgr, err := manager.Setup(mgrOptions)
	require.NoError(t, err)

	go func() {
		err := mgr.Start(t.Context())
		assert.NoError(t, err)
	}()

	namespace := "default" //nolint:goconst,nolintlint
	cmName := "test-configmap"

	t.Log("Create ConfigMap in consumer control plane")
	err = consumer.Cluster.GetClient().Create(
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

	t.Log("Wait for ConfigMap to appear in provider control plane")
	providerCMName := generic.SanitizeClusterName("consumer#consumer#cluster") + "-" + cmName
	require.Eventually(t, func() bool {
		cm := &corev1.ConfigMap{}
		err := provider.Cluster.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      providerCMName,
				Namespace: namespace,
			},
			cm,
		)
		if err != nil {
			t.Logf("error getting configmap from provider control plane: %v", err)
			return false
		}
		return cm.Data["key"] == "value"
	}, wait.ForeverTestTimeout, time.Second)
}
