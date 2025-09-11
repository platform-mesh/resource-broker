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
