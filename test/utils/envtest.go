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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"k8s.io/client-go/rest"

	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// GitRepoRoot returns the root directory of the git repository.
func GitRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(dir + "/.git"); err == nil {
			return dir, nil
		}
		pre := dir
		dir = filepath.Clean(filepath.Join(dir, ".."))
		if pre == dir {
			return "", os.ErrNotExist
		}
	}
}

// EnvOption is an option for configuring the envtest.Environment.
type EnvOption func(*envtest.Environment)

// DefaultEnvTest returns an envtest.Environment with default values
// set and a cleanup function registered to stop the environment when
// the test ends.
func DefaultEnvTest(t testing.TB, opts ...EnvOption) (*envtest.Environment, *rest.Config) {
	gitRoot, err := GitRepoRoot()
	require.NoError(t, err)
	require.NotEmpty(t, gitRoot)

	e := &envtest.Environment{
		BinaryAssetsDirectory: os.Getenv("KUBEBUILDER_ASSETS"),
		CRDDirectoryPaths: []string{
			filepath.Join(gitRoot, "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
	}
	for _, opt := range opts {
		opt(e)
	}
	t.Cleanup(func() {
		if err := e.Stop(); err != nil {
			t.Fatalf("failed to stop envtest: %v", err)
		}
	})
	cfg, err := e.Start()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	return e, cfg
}
