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

package main

import (
	"context"

	mctrl "sigs.k8s.io/multicluster-runtime"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"github.com/platform-mesh/resource-broker/pkg/manager"
)

// wrapperProvider is a workaround until mcr has a better way to
// lifecycle providers.
type wrapperProvider struct {
	multicluster.Provider
	start func(context.Context, mctrl.Manager) error
}

func NewWrappedProvider(p multicluster.Provider, start func(context.Context, mctrl.Manager) error) manager.Starter {
	return &wrapperProvider{
		Provider: p,
		start:    start,
	}
}

func (w *wrapperProvider) Start(ctx context.Context, mgr mctrl.Manager) error {
	return w.start(ctx, mgr)
}
