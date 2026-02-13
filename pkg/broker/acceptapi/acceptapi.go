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

package acceptapi

import (
	"context"

	"github.com/go-logr/logr"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/cluster"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	mctrl "sigs.k8s.io/multicluster-runtime"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
)

// Options defines the options for the AcceptAPI reconciler.
type Options struct {
	GetCluster      func(context.Context, string) (cluster.Cluster, error)
	SetAcceptAPI    func(metav1.GroupVersionResource, string, brokerv1alpha1.AcceptAPI)
	DeleteAcceptAPI func(metav1.GroupVersionResource, string, string)
}

// ReconcilerFunc returns a new reconciler function to handle AcceptAPI
// resources.
func ReconcilerFunc(opts Options) mcreconcile.Func {
	return func(ctx context.Context, req mctrl.Request) (mctrl.Result, error) {
		r := &reconciler{
			opts: opts,
			log: ctrllog.FromContext(ctx).WithValues(
				"clusterName", req.ClusterName,
				"name", req.Name,
				"namespace", req.Namespace,
			),
			req: req,
		}
		return r.reconcile(ctx)
	}
}

const acceptAPIFinalizer = "broker.platform-mesh.io/acceptapi-finalizer"

type reconciler struct {
	opts Options
	log  logr.Logger
	req  mctrl.Request
}

func (r *reconciler) reconcile(ctx context.Context) (mctrl.Result, error) {
	r.log.Info("Reconciling AcceptAPI")

	// TODO Would be better as a handler off of an informer

	cl, err := r.opts.GetCluster(ctx, r.req.ClusterName)
	if err != nil {
		return mctrl.Result{}, err
	}

	acceptAPI := &brokerv1alpha1.AcceptAPI{}
	if err := cl.GetClient().Get(ctx, r.req.NamespacedName, acceptAPI); err != nil {
		if apierrors.IsNotFound(err) {
			return mctrl.Result{}, nil
		}
		return mctrl.Result{}, err
	}

	gvr := acceptAPI.Spec.GVR

	if !acceptAPI.DeletionTimestamp.IsZero() {
		r.log.Info("AcceptAPI is being deleted, removing from apiAccepters map")
		r.opts.DeleteAcceptAPI(gvr, r.req.ClusterName, acceptAPI.Name)

		if ctrlutil.RemoveFinalizer(acceptAPI, acceptAPIFinalizer) {
			if err := cl.GetClient().Update(ctx, acceptAPI); err != nil {
				return mctrl.Result{}, err
			}
		}

		return mctrl.Result{}, nil
	}

	if ctrlutil.AddFinalizer(acceptAPI, acceptAPIFinalizer) {
		if err := cl.GetClient().Update(ctx, acceptAPI); err != nil {
			return mctrl.Result{}, err
		}
	}

	r.opts.SetAcceptAPI(gvr, r.req.ClusterName, *acceptAPI)
	r.log.Info("Cluster already present in apiAccepters map for GVR", "gvr", gvr)
	return mctrl.Result{}, nil
}
