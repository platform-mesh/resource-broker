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

package broker

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	mctrl "sigs.k8s.io/multicluster-runtime"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
	acceptapireconciler "github.com/platform-mesh/resource-broker/pkg/broker/acceptapi"
	genericreconciler "github.com/platform-mesh/resource-broker/pkg/broker/generic"
	"github.com/platform-mesh/resource-broker/pkg/broker/migration"
)

const (
	// ConsumerPrefix is the prefix expected for consumer clusters.
	ConsumerPrefix = "consumer"
	// ProviderPrefix is the prefix expected for provider clusters.
	ProviderPrefix = "provider"
	// CoordinationPrefix is the prefix expected for coordination
	// clusters.
	CoordinationPrefix = "coordination"
)

// Broker brokers API resources to clusters that have accepted given
// APIs.
type Broker struct {
	mgr                   mctrl.Manager
	coordination, compute client.Client
	discoveryClient       discovery.DiscoveryInterface

	lock sync.RWMutex

	// apiAccepters maps GVRs to the names of clusters that accept
	// a given API.
	// GVR -> clusterName -> acceptAPI.Name -> AcceptAPI
	apiAccepters map[metav1.GroupVersionResource]map[string]map[string]brokerv1alpha1.AcceptAPI

	// migrationConfigurations maps source GVKs to target GVKs to
	// MigrationConfigurations.
	// fromGVK -> toGVK -> MigrationConfiguration
	migrationConfigurations map[metav1.GroupVersionKind]map[metav1.GroupVersionKind]brokerv1alpha1.MigrationConfiguration
}

// NewBroker creates a new broker that acts on the given manager.
func NewBroker(
	name string,
	mgr mctrl.Manager,
	coordination, compute client.Client,
	discoveryClient discovery.DiscoveryInterface,
	watchKinds ...string,
) (*Broker, error) {
	b := new(Broker)
	b.mgr = mgr
	b.coordination = coordination
	b.compute = compute
	b.discoveryClient = discoveryClient

	// This map is shared by multiple reconcilers and so needs to be guarded by locks.
	b.apiAccepters = make(map[metav1.GroupVersionResource]map[string]map[string]brokerv1alpha1.AcceptAPI)

	/////////////////////////////////////////////////////////////////////////////
	// AcceptAPI Controller

	acceptAPIOpts := acceptapireconciler.Options{
		GetCluster:           mgr.GetCluster,
		ControllerNamePrefix: name,
		SetAcceptAPI: func(gvr metav1.GroupVersionResource, clusterName string, acceptAPI brokerv1alpha1.AcceptAPI) {
			b.lock.Lock()
			defer b.lock.Unlock()
			if _, ok := b.apiAccepters[gvr]; !ok {
				b.apiAccepters[gvr] = make(map[string]map[string]brokerv1alpha1.AcceptAPI)
			}
			if _, ok := b.apiAccepters[gvr][clusterName]; !ok {
				b.apiAccepters[gvr][clusterName] = make(map[string]brokerv1alpha1.AcceptAPI)
			}
			b.apiAccepters[gvr][clusterName][acceptAPI.Name] = acceptAPI
		},
		DeleteAcceptAPI: func(gvr metav1.GroupVersionResource, clusterName string, acceptAPIName string) {
			b.lock.Lock()
			defer b.lock.Unlock()
			clusterAcceptedAPIs, ok := b.apiAccepters[gvr][clusterName]
			if ok {
				delete(clusterAcceptedAPIs, acceptAPIName)
				if len(clusterAcceptedAPIs) == 0 {
					delete(b.apiAccepters[gvr], clusterName)
				}
			}
		},
	}

	if err := acceptapireconciler.SetupController(mgr, acceptAPIOpts); err != nil {
		return nil, fmt.Errorf("failed to create accept API reconciler: %w", err)
	}

	/////////////////////////////////////////////////////////////////////////////
	// Migration Controllers

	// This map is shared by multiple reconcilers and so needs to be guarded by locks.
	b.migrationConfigurations = make(map[metav1.GroupVersionKind]map[metav1.GroupVersionKind]brokerv1alpha1.MigrationConfiguration)

	migrationConfigOptions := migration.ConfigurationOptions{
		GetCluster:           mgr.GetCluster,
		ControllerNamePrefix: name,
		SetMigrationConfiguration: func(from metav1.GroupVersionKind, to metav1.GroupVersionKind, config brokerv1alpha1.MigrationConfiguration) {
			b.lock.Lock()
			defer b.lock.Unlock()
			if _, ok := b.migrationConfigurations[from]; !ok {
				b.migrationConfigurations[from] = make(map[metav1.GroupVersionKind]brokerv1alpha1.MigrationConfiguration)
			}
			b.migrationConfigurations[from][to] = config
		},
		DeleteMigrationConfiguration: func(from metav1.GroupVersionKind, to metav1.GroupVersionKind) {
			b.lock.Lock()
			defer b.lock.Unlock()
			delete(b.migrationConfigurations[from], to)
			if len(b.migrationConfigurations[from]) == 0 {
				delete(b.migrationConfigurations, from)
			}
		},
	}

	if err := migration.SetupConfigurationController(mgr, migrationConfigOptions); err != nil {
		return nil, fmt.Errorf("failed to create migration reconciler: %w", err)
	}

	migrationOptions := migration.MigrationOptions{
		Compute:                b.compute,
		GetCoordinationCluster: mgr.GetCluster,
		GetProviderCluster:     mgr.GetCluster,
		ControllerNamePrefix:   name,
		GetMigrationConfiguration: func(fromGVK metav1.GroupVersionKind, toGVK metav1.GroupVersionKind) (brokerv1alpha1.MigrationConfiguration, bool) {
			b.lock.RLock()
			defer b.lock.RUnlock()
			toMap, ok := b.migrationConfigurations[fromGVK]
			if !ok {
				return brokerv1alpha1.MigrationConfiguration{}, false
			}
			v, ok := toMap[toGVK]
			return v, ok
		},
	}

	if err := migration.SetupController(mgr, migrationOptions); err != nil {
		return nil, fmt.Errorf("failed to create migration reconciler: %w", err)
	}

	/////////////////////////////////////////////////////////////////////////////
	// Generic Sync Controllers

	genericOpts := genericreconciler.Options{
		CoordinationClient:   b.coordination,
		ControllerNamePrefix: name,
		GetProviderCluster: func(ctx context.Context, clusterName string) (cluster.Cluster, error) {
			if !strings.HasPrefix(clusterName, ProviderPrefix) {
				return nil, fmt.Errorf("cluster %q is not a provider cluster: %w", clusterName, multicluster.ErrClusterNotFound)
			}
			return mgr.GetCluster(ctx, clusterName)
		},
		GetConsumerCluster: func(ctx context.Context, clusterName string) (cluster.Cluster, error) {
			if !strings.HasPrefix(clusterName, ConsumerPrefix) {
				return nil, fmt.Errorf("cluster %q is not a consumer cluster: %w", clusterName, multicluster.ErrClusterNotFound)
			}
			return mgr.GetCluster(ctx, clusterName)
		},
		GetProviders: func(gvr metav1.GroupVersionResource) map[string]map[string]brokerv1alpha1.AcceptAPI {
			b.lock.RLock()
			defer b.lock.RUnlock()
			ret := make(map[string]map[string]brokerv1alpha1.AcceptAPI, len(b.apiAccepters[gvr]))
			for provider, acceptors := range b.apiAccepters[gvr] {
				cloned := make(map[string]brokerv1alpha1.AcceptAPI, len(acceptors))
				maps.Copy(cloned, acceptors)
				ret[provider] = cloned
			}
			return ret
		},
		GetProviderAcceptedAPIs: func(providerName string, gvr metav1.GroupVersionResource) ([]brokerv1alpha1.AcceptAPI, error) {
			b.lock.RLock()
			defer b.lock.RUnlock()
			acceptAPIs := b.apiAccepters[gvr][providerName]
			return slices.Collect(maps.Values(acceptAPIs)), nil
		},
		GetMigrationConfiguration: func(fromGVK metav1.GroupVersionKind, toGVK metav1.GroupVersionKind) (brokerv1alpha1.MigrationConfiguration, bool) {
			b.lock.RLock()
			defer b.lock.RUnlock()
			toMap, ok := b.migrationConfigurations[fromGVK]
			if !ok {
				return brokerv1alpha1.MigrationConfiguration{}, false
			}
			v, ok := toMap[toGVK]
			return v, ok
		},
	}

	for _, gvk := range ParseKinds(watchKinds) {
		if err := genericreconciler.SetupController(mgr, gvk, genericOpts); err != nil {
			return nil, fmt.Errorf("failed to create generic reconciler for %v: %w", gvk, err)
		}
	}

	return b, nil
}
