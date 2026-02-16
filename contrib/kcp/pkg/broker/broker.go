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

	"github.com/go-logr/logr"
	"github.com/kcp-dev/multicluster-provider/apiexport"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"golang.org/x/sync/errgroup"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	mctrl "sigs.k8s.io/multicluster-runtime"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	"sigs.k8s.io/multicluster-runtime/providers/multi"
	"sigs.k8s.io/multicluster-runtime/providers/single"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
	kcpacceptapi "github.com/platform-mesh/resource-broker/contrib/kcp/pkg/acceptapi"
	"github.com/platform-mesh/resource-broker/pkg/broker"
	genericreconciler "github.com/platform-mesh/resource-broker/pkg/broker/generic"
	"github.com/platform-mesh/resource-broker/pkg/broker/migration"
)

// Options are the options for creating a Broker.
type Options struct {
	Name       string
	Log        logr.Logger
	WatchKinds []string

	LocalConfig           *rest.Config
	KcpConfig             *rest.Config
	MigrationCoordination *rest.Config
	ComputeConfig         *rest.Config

	AcceptAPIName string
	BrokerAPIName string

	KcpHostOverride string
	KcpPortOverride string
}

func (o Options) validate() error {
	if o.Name == "" {
		return fmt.Errorf("name is required")
	}
	if o.Log.GetSink() == nil {
		return fmt.Errorf("log is required")
	}
	if o.LocalConfig == nil {
		return fmt.Errorf("local config is required")
	}
	if o.KcpConfig == nil {
		return fmt.Errorf("kcp config is required")
	}
	if o.MigrationCoordination == nil {
		return fmt.Errorf("migration coordination config is required")
	}
	if o.ComputeConfig == nil {
		return fmt.Errorf("compute config is required")
	}
	if o.AcceptAPIName == "" {
		return fmt.Errorf("accept api name is required")
	}
	if o.BrokerAPIName == "" {
		return fmt.Errorf("broker api name is required")
	}
	if len(o.WatchKinds) == 0 {
		return fmt.Errorf("at least one watch kinds is required")
	}
	return nil
}

// Broker brokers API resources to clusters that have accepted given
// APIs.
type Broker struct {
	opts Options

	lock     sync.RWMutex
	managers map[string]mctrl.Manager

	// apiAccepters maps GVRs to the names of clusters that accept
	// a given API.
	// GVR -> clusterName -> acceptAPI.Name -> AcceptAPI
	apiAccepters map[metav1.GroupVersionResource]map[string]map[string]brokerv1alpha1.AcceptAPI

	// migrationConfigurations maps source GVKs to target GVKs to
	// MigrationConfigurations.
	// fromGVK -> toGVK -> MigrationConfiguration
	migrationConfigurations map[metav1.GroupVersionKind]map[metav1.GroupVersionKind]brokerv1alpha1.MigrationConfiguration
}

// New creates a new broker that acts on the given manager.
func New(opts Options) (*Broker, error) { //nolint:gocyclo
	if err := opts.validate(); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}

	b := new(Broker)
	b.opts = opts
	b.managers = make(map[string]mctrl.Manager)

	multiProvider := multi.New(multi.Options{})

	/////////////////////////////////////////////////////////////////////////////
	// AcceptAPI Controller

	// The kcp AcceptAPI provider watches the VW of the AcceptAPI export and
	// produces provider clusters.
	b.apiAccepters = make(map[metav1.GroupVersionResource]map[string]map[string]brokerv1alpha1.AcceptAPI)
	acceptAPIScheme := runtime.NewScheme()
	if err := brokerv1alpha1.AddToScheme(acceptAPIScheme); err != nil {
		return nil, fmt.Errorf("unable to add broker v1alpha1 to acceptapi scheme: %w", err)
	}
	if err := kcpapisv1alpha1.AddToScheme(acceptAPIScheme); err != nil {
		return nil, fmt.Errorf("unable to add kcp apis to acceptapi scheme: %w", err)
	}
	if err := kcpapisv1alpha2.AddToScheme(acceptAPIScheme); err != nil {
		return nil, fmt.Errorf("unable to add kcp apis to acceptapi scheme: %w", err)
	}
	if err := clientgoscheme.AddToScheme(acceptAPIScheme); err != nil {
		return nil, fmt.Errorf("unable to add client-go scheme to acceptapi scheme: %w", err)
	}

	kcpAcceptAPI, err := kcpacceptapi.New(kcpacceptapi.Options{
		KcpConfig:     opts.KcpConfig,
		APIExportName: opts.AcceptAPIName,
		Scheme:        acceptAPIScheme,
		HostOverride:  opts.KcpHostOverride,
		PortOverride:  opts.KcpPortOverride,
		SetAcceptAPI: func(gvr metav1.GroupVersionResource, clusterName string, acceptAPI brokerv1alpha1.AcceptAPI) {
			// Workaround - the resulting cluster is added to the multi provider
			// here, which is in turn added to another multi provider with
			// a prefix. So if the reconciler using that second multi provider
			// tries to look up the cluster by name that fails as the prefix is
			// missing.
			// So we add a prefix when storing the cluster AcceptAPI.
			clusterName = broker.ProviderPrefix + "#" + clusterName
			b.opts.Log.Info("SetAcceptAPI", "gvr", gvr, "cluster", clusterName, "acceptAPI", acceptAPI.Name)
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
			// Workaround - the resulting cluster is added to the multi provider
			// here, which is in turn added to another multi provider with
			// a prefix. So if the reconciler using that second multi provider
			// tries to look up the cluster by name that fails as the prefix is
			// missing.
			// So we add a prefix when storing the cluster AcceptAPI.
			clusterName = broker.ProviderPrefix + "#" + clusterName
			b.opts.Log.Info("DeleteAcceptAPI", "gvr", gvr, "cluster", clusterName, "acceptAPI", acceptAPIName)
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
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create acceptapi provider: %w", err)
	}

	kcpAcceptAPIMgr, err := mcmanager(opts.LocalConfig, acceptAPIScheme, kcpAcceptAPI.Input)
	if err != nil {
		return nil, fmt.Errorf("unable to create acceptapi manager: %w", err)
	}
	if err := mcbuilder.ControllerManagedBy(kcpAcceptAPIMgr).
		Named(b.opts.Name + "-kcp-acceptapi").
		For(&brokerv1alpha1.AcceptAPI{}).
		Complete(kcpAcceptAPI); err != nil {
		return nil, fmt.Errorf("failed to create acceptapi reconciler: %w", err)
	}

	b.managers["kcp-acceptapi"] = kcpAcceptAPIMgr
	if err := multiProvider.AddProvider(broker.ProviderPrefix, kcpAcceptAPI.Output); err != nil {
		return nil, fmt.Errorf("error adding acceptapi provider to multi provider: %w", err)
	}

	/////////////////////////////////////////////////////////////////////////////
	// Migration Controllers

	b.migrationConfigurations = make(map[metav1.GroupVersionKind]map[metav1.GroupVersionKind]brokerv1alpha1.MigrationConfiguration)
	// using the migrationScheme for both migration and migration config
	migrationScheme := runtime.NewScheme()
	if err := brokerv1alpha1.AddToScheme(migrationScheme); err != nil {
		return nil, fmt.Errorf("unable to add broker v1alpha1 to migration scheme: %w", err)
	}
	migrationClient, err := client.New(opts.MigrationCoordination, client.Options{
		Scheme: migrationScheme,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating migration coordination client: %w", err)
	}
	migrationCluster, err := cluster.New(b.opts.MigrationCoordination,
		func(o *cluster.Options) {
			o.Scheme = migrationClient.Scheme()
		},
	)
	if err != nil {
		return nil, fmt.Errorf("error creating migration coordination cluster: %w", err)
	}
	migrationProvider := single.New("migration-coordination", migrationCluster)
	migrationMgr, err := mcmanager(opts.MigrationCoordination, migrationClient.Scheme(), migrationProvider)
	if err != nil {
		return nil, fmt.Errorf("unable to create migration manager: %w", err)
	}
	if err := migrationMgr.GetLocalManager().Add(manager.RunnableFunc(migrationCluster.Start)); err != nil {
		return nil, fmt.Errorf("error adding migration coordination cluster to migration manager: %w", err)
	}

	// b.managers["migration"] = migrationMgr

	migrationConfigOptions := migration.ConfigurationOptions{
		GetCluster:           migrationMgr.GetCluster, // migration configurations can only come from the migration coordination cluster
		ControllerNamePrefix: b.opts.Name,
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

	if err := migration.SetupConfigurationController(migrationMgr, migrationConfigOptions); err != nil {
		return nil, fmt.Errorf("failed to create migration reconciler: %w", err)
	}

	computeClient, err := client.New(b.opts.ComputeConfig, client.Options{
		Scheme: runtime.NewScheme(),
	})
	if err != nil {
		return nil, fmt.Errorf("error creating compute client: %w", err)
	}

	migrationOptions := migration.MigrationOptions{
		Compute:                computeClient,
		ControllerNamePrefix:   b.opts.Name,
		GetCoordinationCluster: migrationMgr.GetCluster,
		GetProviderCluster: func(ctx context.Context, clusterName string) (cluster.Cluster, error) {
			if !strings.HasPrefix(clusterName, broker.ProviderPrefix) {
				return nil, fmt.Errorf("cluster %q is not a provider cluster: %w", clusterName, multicluster.ErrClusterNotFound)
			}
			return multiProvider.Get(ctx, clusterName)
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

	if err := migration.SetupController(migrationMgr, migrationOptions); err != nil {
		return nil, fmt.Errorf("failed to create migration reconciler: %w", err)
	}

	/////////////////////////////////////////////////////////////////////////////
	// Generic Sync Controllers

	// The BrokerAPI provider watches the VW of the BrokerAPI export and
	// produces consumer clusters.
	brokerAPIScheme := runtime.NewScheme()
	if err := kcpapisv1alpha1.AddToScheme(brokerAPIScheme); err != nil {
		return nil, fmt.Errorf("unable to add kcp apis to acceptapi scheme: %w", err)
	}
	if err := kcpapisv1alpha2.AddToScheme(brokerAPIScheme); err != nil {
		return nil, fmt.Errorf("unable to add kcp apis to broker api scheme: %w", err)
	}
	brokerAPIs, err := apiexport.New(opts.KcpConfig, opts.BrokerAPIName, apiexport.Options{
		Scheme: brokerAPIScheme,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create brokerapi provider: %w", err)
	}
	if err := multiProvider.AddProvider(broker.ConsumerPrefix, brokerAPIs); err != nil {
		return nil, fmt.Errorf("error adding brokerapi provider to multi provider: %w", err)
	}

	generalScheme := runtime.NewScheme()
	generalMgr, err := mcmanager(opts.LocalConfig, generalScheme, multiProvider)
	if err != nil {
		return nil, fmt.Errorf("unable to create general broker manager: %w", err)
	}
	b.managers["general"] = generalMgr

	genericOpts := genericreconciler.Options{
		CoordinationClient:   migrationClient,
		ControllerNamePrefix: b.opts.Name,
		GetProviderCluster: func(ctx context.Context, clusterName string) (cluster.Cluster, error) {
			if !strings.HasPrefix(clusterName, broker.ProviderPrefix) {
				return nil, fmt.Errorf("cluster %q is not a provider cluster: %w", clusterName, multicluster.ErrClusterNotFound)
			}
			b.opts.Log.Info("GetProviderCluster", "clusterName", clusterName)
			return multiProvider.Get(ctx, clusterName)
		},
		GetConsumerCluster: func(ctx context.Context, clusterName string) (cluster.Cluster, error) {
			if !strings.HasPrefix(clusterName, broker.ConsumerPrefix) {
				return nil, fmt.Errorf("cluster %q is not a consumer cluster: %w", clusterName, multicluster.ErrClusterNotFound)
			}
			return multiProvider.Get(ctx, clusterName)
		},
		GetProviders: func(gvr metav1.GroupVersionResource) map[string]map[string]brokerv1alpha1.AcceptAPI {
			b.lock.RLock()
			defer b.lock.RUnlock()
			ret := make(map[string]map[string]brokerv1alpha1.AcceptAPI, len(b.apiAccepters[gvr]))
			for providerClusterName, acceptors := range b.apiAccepters[gvr] {
				cloned := make(map[string]brokerv1alpha1.AcceptAPI, len(acceptors))
				maps.Copy(cloned, acceptors)
				ret[providerClusterName] = cloned
			}
			return ret
		},
		GetProviderAcceptedAPIs: func(providerName string, gvr metav1.GroupVersionResource) ([]brokerv1alpha1.AcceptAPI, error) {
			b.lock.RLock()
			defer b.lock.RUnlock()
			b.opts.Log.Info("GetProviderAcceptedAPIs", "providerName", providerName, "gvr", gvr, "storedProviders", slices.Sorted(maps.Keys(b.apiAccepters[gvr])))
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

	for _, gvk := range broker.ParseKinds(b.opts.WatchKinds) {
		if err := genericreconciler.SetupController(generalMgr, gvk, genericOpts); err != nil {
			return nil, fmt.Errorf("failed to create generic reconciler for %v: %w", gvk, err)
		}
	}

	return b, nil
}

// Start starts all managers of the broker.
func (b *Broker) Start(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)
	for _, mgr := range b.managers {
		g.Go(func() error {
			return mgr.Start(ctx)
		})
	}
	return g.Wait()
}
