//go:build kube_legacy

// Package manager is a stub kept solely so the kube_legacy-gated test/e2e
// suite still compiles. The real package was removed in #342; the e2e
// suite is preserved verbatim pending a kcp-based rewrite, with each test
// guarded by t.Skip. This stub makes 'go mod tidy' resolve cleanly under
// every build-tag combination.
package manager

import (
	"k8s.io/client-go/rest"

	mctrl "sigs.k8s.io/multicluster-runtime"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

type Options struct {
	MgrOptions         mctrl.Options
	Name               string
	Local              *rest.Config
	ComputeConfig      *rest.Config
	CoordinationConfig *rest.Config
	Consumer, Provider multicluster.Provider
	WatchKinds         []string
}

func Setup(_ Options) (mctrl.Manager, error) { return nil, nil }
