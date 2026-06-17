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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	mctrl "sigs.k8s.io/multicluster-runtime"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

// manager creates a new multicluster manager that disables metrics
// etcpp. to allow running multiple manager instances in the same
// process.
// Background is that mcr stops clusters if a cluster doesn't have all
// of the resources installed it expects.
// Since e.g. the VW for AcceptAPI won't have the brokered resources
// installed this will always result in failures.
func mcmanager(local *rest.Config, scheme *runtime.Scheme, provider multicluster.Provider) (mctrl.Manager, error) {
	return mctrl.NewManager(
		local,
		provider,
		mctrl.Options{
			Scheme:           scheme,
			PprofBindAddress: "0", // disable pprof
			Controller: config.Controller{
				SkipNameValidation: ptr.To(true),
			},
			Metrics: metricsserver.Options{
				BindAddress: "0", // disable metrics
			},
		},
	)
}
