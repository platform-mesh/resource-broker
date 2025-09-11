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

package utils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"k8s.io/client-go/rest"

	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// DefaultEnvTest returns an envtest.Environment with default values
// set and a cleanup function registered to stop the environment when
// the test ends.
func DefaultEnvTest(t testing.TB) *envtest.Environment {
	e := &envtest.Environment{
		BinaryAssetsDirectory: os.Getenv("KUBEBUILDER_ASSETS"),
		CRDDirectoryPaths: []string{
			"config/crd/bases",
		},
	}
	t.Cleanup(func() {
		if err := e.Stop(); err != nil {
			t.Fatalf("failed to stop envtest: %v", err)
		}
	})
	return e
}

// DefaultEnvTestStart returns the environment as defaulted by
// DefaultEnvTest and starts it.
func DefaultEnvTestStart(t testing.TB) (*envtest.Environment, *rest.Config) {
	e := DefaultEnvTest(t)
	cfg, err := e.Start()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	return e, cfg
}
