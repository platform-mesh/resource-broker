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

package broker

import (
	"context"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	mctrl "sigs.k8s.io/multicluster-runtime"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
)

func (b *Broker) acceptAPIReconciler(mgr mctrl.Manager) error {
	return mcbuilder.ControllerManagedBy(mgr).
		Named("acceptapi").
		For(&brokerv1alpha1.AcceptAPI{}).
		Complete(mcreconcile.Func(b.acceptAPIReconcile))
}

// +kubebuilder:rbac:groups=broker.platform-mesh.io,resources=acceptapis,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=broker.platform-mesh.io,resources=acceptapis/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=broker.platform-mesh.io,resources=acceptapis/finalizers,verbs=update

const acceptAPIFinalizer = "broker.platform-mesh.io/acceptapi-finalizer"

func (b *Broker) acceptAPIReconcile(ctx context.Context, req mctrl.Request) (mctrl.Result, error) {
	log := ctrllog.FromContext(ctx).WithValues("cluster", req.ClusterName)
	log.Info("Reconciling AcceptAPI")

	cl, err := b.mgr.GetCluster(ctx, req.ClusterName)
	if err != nil {
		return mctrl.Result{}, err
	}

	acceptAPI := &brokerv1alpha1.AcceptAPI{}
	if err := cl.GetClient().Get(ctx, req.NamespacedName, acceptAPI); err != nil {
		if apierrors.IsNotFound(err) {
			return mctrl.Result{}, nil
		}
		return mctrl.Result{}, err
	}

	gvr := acceptAPI.Spec.GVR

	if !acceptAPI.DeletionTimestamp.IsZero() {
		log.Info("AcceptAPI is being deleted, removing from apiAccepters map")

		b.lock.Lock()
		b.apiAccepters[gvr] = slices.DeleteFunc(
			b.apiAccepters[gvr],
			func(s string) bool {
				return s == req.ClusterName
			},
		)
		b.lock.Unlock()

		if ctrlutil.RemoveFinalizer(acceptAPI, acceptAPIFinalizer) {
			if err := cl.GetClient().Update(ctx, acceptAPI); err != nil {
				return mctrl.Result{}, err
			}
		}

		return mctrl.Result{}, nil
	}

	b.lock.Lock()
	if !slices.Contains(b.apiAccepters[gvr], req.ClusterName) {
		b.apiAccepters[gvr] = append(
			b.apiAccepters[gvr],
			req.ClusterName,
		)
		log.Info("Added cluster to apiAccepters map for GVR", "gvr", gvr)
	}
	b.lock.Unlock()

	if ctrlutil.AddFinalizer(acceptAPI, acceptAPIFinalizer) {
		if err := cl.GetClient().Update(ctx, acceptAPI); err != nil {
			return mctrl.Result{}, err
		}
	}

	log.Info("Cluster already present in apiAccepters map for GVR", "gvr", gvr)
	return mctrl.Result{}, nil
}
