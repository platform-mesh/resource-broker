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

package broker

import (
	"fmt"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	mctrl "sigs.k8s.io/multicluster-runtime"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
)

// Broker brokers API resources to clusters that have accepted given
// APIs.
type Broker struct {
	mgr mctrl.Manager

	lock sync.RWMutex

	// apiAccepters maps GVRs to the names of clusters that accept
	// a given API.
	// GVR -> clusterName -> acceptAPI.Name -> AcceptAPI
	apiAccepters map[metav1.GroupVersionResource]map[string]map[string]*brokerv1alpha1.AcceptAPI
}

// NewBroker creates a new broker that acts on the given manager.
func NewBroker(mgr mctrl.Manager, gvks ...schema.GroupVersionKind) (*Broker, error) {
	b := new(Broker)
	b.mgr = mgr
	b.apiAccepters = make(map[metav1.GroupVersionResource]map[string]map[string]*brokerv1alpha1.AcceptAPI)

	if err := b.acceptAPIReconciler(mgr); err != nil {
		return nil, err
	}

	for _, gvk := range gvks {
		if err := b.genericReconciler(mgr, gvk); err != nil {
			return nil, fmt.Errorf("failed to create generic reconciler for %v: %w", gvk, err)
		}
	}

	return b, nil
}
