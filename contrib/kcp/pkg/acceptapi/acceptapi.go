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
// [brokerv1alpha1.AcceptAPI] resources in a kcp VW and provides the
// resulting clusters as a [multicluster.Provider].
package acceptapi

import (
	"context"
	"fmt"
	"net"
	"net/url"

	"github.com/kcp-dev/multicluster-provider/apiexport"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	mctrl "sigs.k8s.io/multicluster-runtime"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
	"sigs.k8s.io/multicluster-runtime/providers/clusters"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
)

// Options defines the options for the AcceptAPI reconciler.
type Options struct {
	KcpConfig       *rest.Config
	APIExportName   string
	Scheme          *runtime.Scheme
	SetAcceptAPI    func(metav1.GroupVersionResource, string, brokerv1alpha1.AcceptAPI)
	DeleteAcceptAPI func(metav1.GroupVersionResource, string, string)

	HostOverride string
	PortOverride string
}

func (o *Options) validate() error {
	if o.KcpConfig == nil {
		return fmt.Errorf("KcpConfig is required")
	}
	if o.APIExportName == "" {
		return fmt.Errorf("APIExportName is required")
	}
	if o.Scheme == nil {
		o.Scheme = scheme.Scheme
	}
	if o.SetAcceptAPI == nil {
		return fmt.Errorf("SetAcceptAPI is required")
	}
	if o.DeleteAcceptAPI == nil {
		return fmt.Errorf("DeleteAcceptAPI is required")
	}
	return nil
}

const kcpAcceptAPIFinalizer = "broker.platform-mesh.io/kcp-acceptapi-finalizer"

// Reconciler implements the kcp AcceptAPI reconciler.
type Reconciler struct {
	opts Options

	Input  *apiexport.Provider
	Output *clusters.Provider
}

// New creates a new AcceptAPI reconciler.
func New(opts Options) (*Reconciler, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	r := new(Reconciler)
	r.opts = opts
	r.Output = clusters.New()
	r.Output.EqualClusters = areClustersEqual

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
	log.Info("Reconciling AcceptAPI for VW provider")

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
		log.Info("AcceptAPI is being deleted, removing VW provider")
		r.opts.DeleteAcceptAPI(gvr, req.ClusterName, acceptAPI.Name)
		r.Output.Remove(req.ClusterName)
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

	secretName, ok := acceptAPI.Annotations["broker.platform-mesh.io/secret-name"]
	if !ok || secretName == "" {
		log.Error(nil, "AcceptAPI is missing broker.platform-mesh.io/secret-name annotation")
		return mctrl.Result{}, fmt.Errorf("AcceptAPI %s/%s is missing broker.platform-mesh.io/secret-name annotation", acceptAPI.Namespace, acceptAPI.Name)
	}

	secret := &corev1.Secret{}
	if err := cl.GetClient().Get(
		ctx,
		types.NamespacedName{
			Namespace: "default",
			Name:      secretName,
		},
		secret,
	); err != nil {
		log.Error(err, "Error getting kubeconfig secret")
		return mctrl.Result{}, err
	}

	log = log.WithValues("secret", secret.Name)

	log.Info("Found secret for cluster")
	rawKubeconfig, ok := secret.Data["kubeconfig"]
	if !ok {
		log.Error(nil, "Secret is missing kubeconfig data")
		return mctrl.Result{}, fmt.Errorf("secret %s/%s is missing kubeconfig data", secret.Namespace, secret.Name)
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(rawKubeconfig)
	if err != nil {
		log.Error(err, "Error creating REST config from kubeconfig")
		return mctrl.Result{}, err
	}
	log.Info("Parsed kubeconfig from secret", "cfg", cfg)

	if r.opts.HostOverride != "" || r.opts.PortOverride != "" {
		u, err := url.Parse(cfg.Host)
		if err != nil {
			log.Error(err, "Error parsing host from kubeconfig")
			return mctrl.Result{}, err
		}
		host, port, err := net.SplitHostPort(u.Host)
		if err != nil {
			log.Error(err, "Error splitting host and port from kubeconfig host")
			return mctrl.Result{}, err
		}
		if r.opts.HostOverride != "" {
			host = r.opts.HostOverride
		}
		if r.opts.PortOverride != "" {
			port = r.opts.PortOverride
		}
		u.Host = net.JoinHostPort(host, port)
		cfg.Host = u.String()
	}

	vwCluster, err := cluster.New(cfg,
		func(o *cluster.Options) {
			o.Scheme = r.opts.Scheme
		},
	)
	if err != nil {
		log.Error(err, "Error creating VW cluster")
		return mctrl.Result{}, err
	}

	// Force discovery cache population by querying the RESTMapper
	// This ensures the discovery cache knows about the resources before we add the cluster to the provider
	// We use KindFor which will trigger a full API discovery if the cache is empty
	_, err = vwCluster.GetRESTMapper().KindFor(schema.GroupVersionResource{
		Group:    gvr.Group,
		Version:  gvr.Version,
		Resource: gvr.Resource,
	})
	if err != nil {
		log.Info("Discovery cache refresh failed, will retry", "error", err.Error(), "gvr", gvr)
		// Don't fail here - the resource might not be immediately available
		// The controller will retry and eventually succeed
	} else {
		log.Info("Successfully populated discovery cache", "gvr", gvr)
	}

	log.Info("Adding VW cluster to provider")
	r.opts.SetAcceptAPI(gvr, req.ClusterName, *acceptAPI)
	if err := r.Output.Add(ctx, req.ClusterName, vwCluster); err != nil {
		log.Error(err, "Error adding VW cluster to provider")
		return mctrl.Result{}, err
	}
	return mctrl.Result{}, nil
}

func areClustersEqual(cl1, cl2 cluster.Cluster) bool {
	if cl1 == cl2 {
		return true
	}
	return areConfigsEqual(cl1.GetConfig(), cl2.GetConfig())
}

func areConfigsEqual(cfg1, cfg2 *rest.Config) bool {
	if cfg1 == nil || cfg2 == nil {
		return cfg1 == cfg2
	}
	if cfg1.String() != cfg2.String() {
		return false
	}
	if cfg1.Password != cfg2.Password {
		return false
	}
	if cfg1.BearerToken != cfg2.BearerToken {
		return false
	}
	return true
}
