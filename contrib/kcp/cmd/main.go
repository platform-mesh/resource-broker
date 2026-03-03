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

// The main package running the resource-broker.
package main

import (
	"flag"
	"maps"
	"os"
	"slices"

	"k8s.io/client-go/tools/clientcmd"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	mctrl "sigs.k8s.io/multicluster-runtime"

	kcpbroker "github.com/platform-mesh/resource-broker/contrib/kcp/pkg/broker"
	"github.com/platform-mesh/resource-broker/pkg/version"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var (
	setupLog = ctrl.Log.WithName("setup")

	fKcpKubeconfig = flag.String(
		"kcp-kubeconfig",
		"",
		"Kubeconfig for the coordination cluster. If not set, in-cluster config will be used.",
	)
	fComputeKubeconfig = flag.String(
		"compute-kubeconfig",
		"",
		"Kubeconfig for the compute cluster. If not set, in-cluster config will be used.",
	)

	fAcceptAPI = flag.String(
		"acceptapi",
		"",
		"APIExportEndpointSlice name to watch for AcceptAPIs.",
	)
	fBrokerAPI = flag.String(
		"brokerapi",
		"",
		"APIExportEndpointSlice name to watch for APIs to broker.",
	)

	fOverrideKcpHost = flag.String("kcp-host-override", "", "If set, overrides the host used to connect to kcp")
	fOverrideKcpPort = flag.String("kcp-port-override", "", "If set, overrides the port used to connect to kcp")
)

func main() {
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)

	watchKinds := map[string]bool{}
	flag.Func("watch-kind", "Kind to watch in the form of Kind.version.group", func(s string) error {
		watchKinds[s] = true
		return nil
	})

	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog.Info("Starting resource-broker", "version", version.Info())

	ctx := mctrl.SetupSignalHandler()

	local, err := ctrl.GetConfig()
	if err != nil {
		setupLog.Error(err, "unable to get local kubeconfig")
		os.Exit(1)
	}

	computeConfig := local
	if *fComputeKubeconfig != "" {
		rawComputeConfig, err := clientcmd.LoadFromFile(*fComputeKubeconfig)
		if err != nil {
			setupLog.Error(err, "unable to load compute kubeconfig", "path", *fComputeKubeconfig)
			os.Exit(1)
		}

		computeConfig, err = clientcmd.NewNonInteractiveClientConfig(
			*rawComputeConfig,
			rawComputeConfig.CurrentContext,
			&clientcmd.ConfigOverrides{},
			nil,
		).ClientConfig()
		if err != nil {
			setupLog.Error(err, "unable to create compute rest config")
			os.Exit(1)
		}
	}

	kcpConfig := local
	if *fKcpKubeconfig != "" {
		rawCoordinationConfig, err := clientcmd.LoadFromFile(*fKcpKubeconfig)
		if err != nil {
			setupLog.Error(err, "unable to load coordination kubeconfig", "path", *fKcpKubeconfig)
			os.Exit(1)
		}

		kcpConfig, err = clientcmd.NewNonInteractiveClientConfig(
			*rawCoordinationConfig,
			rawCoordinationConfig.CurrentContext,
			&clientcmd.ConfigOverrides{},
			nil,
		).ClientConfig()
		if err != nil {
			setupLog.Error(err, "unable to create coordination rest config")
			os.Exit(1)
		}
	}

	brk, err := kcpbroker.New(kcpbroker.Options{
		Name:       "kcp-main",
		Log:        setupLog.WithName("broker"),
		WatchKinds: slices.Collect(maps.Keys(watchKinds)),

		LocalConfig:           local,
		KcpConfig:             kcpConfig,
		MigrationCoordination: kcpConfig,
		ComputeConfig:         computeConfig,

		AcceptAPIName: *fAcceptAPI,
		BrokerAPIName: *fBrokerAPI,

		KcpHostOverride: *fOverrideKcpHost,
		KcpPortOverride: *fOverrideKcpPort,
	})
	if err != nil {
		setupLog.Error(err, "unable to setup broker")
		os.Exit(1)
	}

	if err := brk.Start(ctx); err != nil {
		setupLog.Error(err, "exiting due to error")
		os.Exit(1)
	}
}
