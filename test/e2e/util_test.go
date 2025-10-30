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
	"crypto/rand"

	"k8s.io/utils/ptr"

	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	mctrl "sigs.k8s.io/multicluster-runtime"
)

func init() {
	log.SetLogger(zap.New(zap.UseDevMode(true)))
}

// ManagerOptions returns the default manager options for tests.
func ManagerOptions() mctrl.Options {
	return mctrl.Options{
		LeaderElectionID: rand.Text(),
		Metrics: metricsserver.Options{
			BindAddress: "0", // disable metrics
		},
		Controller: ctrlconfig.Controller{
			SkipNameValidation: ptr.To(true), // skip name validation of controller metrics for tests
		},
	}
}
