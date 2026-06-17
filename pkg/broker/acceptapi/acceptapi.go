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

// Package acceptapi implements a reconciler that watches
// [brokerv1alpha1.AcceptAPI] resources in a kcp VW and registers the
// provider's metadata (workspace path and APIExport name) so that the broker
// can create staging workspaces on demand.
package acceptapi

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	kcpcore "github.com/kcp-dev/sdk/apis/core"
	corev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	mctrl "sigs.k8s.io/multicluster-runtime"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
)

const (
	kcpAcceptAPIFinalizer = "broker.platform-mesh.io/kcp-acceptapi-finalizer"

	// AnnotationAPIExportName is the annotation providers set on their
	// AcceptAPI objects to indicate which APIExport rb should bind in the
	// per-consumer staging workspace.
	AnnotationAPIExportName = "broker.platform-mesh.io/kcp-apiexport-name"
)

// Options defines the options for the AcceptAPI reconciler.
type Options struct {
	KcpConfig       *rest.Config
	APIExportName   string
	Scheme          *runtime.Scheme
	SetAcceptAPI    func(metav1.GroupVersionResource, multicluster.ClusterName, brokerv1alpha1.AcceptAPI)
	DeleteAcceptAPI func(metav1.GroupVersionResource, multicluster.ClusterName, string)
}

func (o *Options) validate() error {
	if o.KcpConfig == nil {
		return fmt.Errorf("KcpConfig is required")
	}
	if o.APIExportName == "" {
		return fmt.Errorf("APIExportName is required")
	}
	if o.Scheme == nil {
		return fmt.Errorf("scheme is required")
	}
	if o.SetAcceptAPI == nil {
		return fmt.Errorf("SetAcceptAPI is required")
	}
	if o.DeleteAcceptAPI == nil {
		return fmt.Errorf("DeleteAcceptAPI is required")
	}
	return nil
}

// Reconciler implements the kcp AcceptAPI reconciler.
type Reconciler struct {
	opts Options

	Input      *apiexport.Provider
	coreScheme *runtime.Scheme
}

// New creates a new AcceptAPI reconciler.
func New(opts Options) (*Reconciler, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	r := new(Reconciler)
	r.opts = opts

	r.coreScheme = runtime.NewScheme()
	if err := corev1alpha1.AddToScheme(r.coreScheme); err != nil {
		return nil, fmt.Errorf("unable to add core v1alpha1 to scheme: %w", err)
	}

	var err error
	r.Input, err = apiexport.New(opts.KcpConfig, opts.APIExportName, apiexport.Options{
		Scheme: opts.Scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create acceptapi apiexport provider: %w", err)
	}

	return r, nil
}

// Reconcile reconciles AcceptAPI resources.
func (r *Reconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (mctrl.Result, error) {
	log := ctrllog.FromContext(ctx).WithValues(
		"clusterName", req.ClusterName,
		"namespace", req.Namespace,
		"name", req.Name,
	)
	log.Info("Reconciling AcceptAPI")

	cl, err := r.Input.Get(ctx, req.ClusterName)
	if err != nil {
		log.Error(err, "Error getting cluster from APIExport provider")
		return mctrl.Result{}, err
	}

	acceptAPI := &brokerv1alpha1.AcceptAPI{}
	if err := cl.GetClient().Get(ctx, req.NamespacedName, acceptAPI); err != nil {
		if apierrors.IsNotFound(err) {
			return mctrl.Result{}, nil
		}
		log.Error(err, "Error getting AcceptAPI")
		return mctrl.Result{}, err
	}
	gvr := acceptAPI.Spec.GVR

	if !acceptAPI.DeletionTimestamp.IsZero() {
		log.Info("AcceptAPI is being deleted")
		r.opts.DeleteAcceptAPI(gvr, req.ClusterName, acceptAPI.Name)
		if controllerutil.RemoveFinalizer(acceptAPI, kcpAcceptAPIFinalizer) {
			if err := cl.GetClient().Update(ctx, acceptAPI); err != nil {
				return mctrl.Result{}, err
			}
		}
		return mctrl.Result{}, nil
	}

	if controllerutil.AddFinalizer(acceptAPI, kcpAcceptAPIFinalizer) {
		log.Info("Adding finalizer to AcceptAPI")
		if err := cl.GetClient().Update(ctx, acceptAPI); err != nil {
			return mctrl.Result{}, err
		}
	}

	if acceptAPI.Annotations[AnnotationAPIExportName] == "" {
		log.Error(nil, "AcceptAPI is missing broker.platform-mesh.io/kcp-apiexport-name annotation")
		return mctrl.Result{}, fmt.Errorf("AcceptAPI %s/%s is missing %s annotation", acceptAPI.Namespace, acceptAPI.Name, AnnotationAPIExportName)
	}

	// Derive the provider workspace path from the LogicalCluster singleton
	// rather than requiring the provider to annotate it manually.
	providerPath, err := r.lookupProviderPath(ctx, string(req.ClusterName))
	if err != nil {
		log.Error(err, "Failed to look up provider workspace path")
		return mctrl.Result{}, err
	}

	if acceptAPI.Annotations == nil {
		acceptAPI.Annotations = make(map[string]string)
	}
	acceptAPI.Annotations[kcpcore.LogicalClusterPathAnnotationKey] = providerPath

	log.Info("Registering AcceptAPI",
		"providerPath", providerPath,
		"apiExportName", acceptAPI.Annotations[AnnotationAPIExportName],
	)
	r.opts.SetAcceptAPI(gvr, req.ClusterName, *acceptAPI)

	return mctrl.Result{}, nil
}

// lookupProviderPath fetches the workspace path for the given kcp logical
// cluster ID by reading the LogicalCluster singleton resource directly in
// that workspace. kcp sets kcp.io/path on the LogicalCluster to the
// human-readable workspace path (e.g. "root:internalca").
func (r *Reconciler) lookupProviderPath(ctx context.Context, clusterID string) (string, error) {
	cfg, err := clusterDirectConfig(r.opts.KcpConfig, clusterID)
	if err != nil {
		return "", fmt.Errorf("failed to build cluster config for %q: %w", clusterID, err)
	}

	cl, err := client.New(cfg, client.Options{Scheme: r.coreScheme})
	if err != nil {
		return "", fmt.Errorf("failed to create client for cluster %q: %w", clusterID, err)
	}

	lc := &corev1alpha1.LogicalCluster{}
	if err := cl.Get(ctx, types.NamespacedName{Name: corev1alpha1.LogicalClusterName}, lc); err != nil {
		return "", fmt.Errorf("failed to get LogicalCluster for cluster %q: %w", clusterID, err)
	}

	path := lc.Annotations[kcpcore.LogicalClusterPathAnnotationKey]
	if path == "" {
		return "", fmt.Errorf("LogicalCluster for cluster %q has no %s annotation", clusterID, kcpcore.LogicalClusterPathAnnotationKey)
	}
	return path, nil
}

// clusterDirectConfig derives a REST config that directly accesses the given
// KCP logical cluster ID by replacing the cluster path segment in the base URL.
func clusterDirectConfig(base *rest.Config, clusterID string) (*rest.Config, error) {
	cfg := rest.CopyConfig(base)
	u, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to parse KCP host URL %q: %w", cfg.Host, err)
	}
	idx := strings.Index(u.Path, "/clusters/")
	if idx < 0 {
		return nil, fmt.Errorf("KCP host URL %q does not contain /clusters/ path segment", cfg.Host)
	}
	u.Path = u.Path[:idx] + "/clusters/" + clusterID
	cfg.Host = u.String()
	return cfg, nil
}
