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
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mctrl "sigs.k8s.io/multicluster-runtime"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
)

func (b *Broker) genericReconciler(mgr mctrl.Manager, gvk schema.GroupVersionKind) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	// TODO register gck in broker
	return mcbuilder.ControllerManagedBy(mgr).
		Named("generic-" + gvk.String()).
		For(obj).
		Complete(b.genericReconcilerFactory(gvk))
}

const (
	genericFinalizer   = "broker.platform-mesh.io/generic-finalizer"
	providerClusterAnn = "broker.platform-mesh.io/provider-cluster"
)

func (b *Broker) genericReconcilerFactory(gvk schema.GroupVersionKind) reconcile.TypedReconciler[mcreconcile.Request] {
	return mcreconcile.Func(
		func(ctx context.Context, req mctrl.Request) (mctrl.Result, error) {
			log := ctrllog.FromContext(ctx).WithValues("cluster", req.ClusterName)
			log.Info("Reconciling generic resource", "gvk", gvk)

			cl, err := b.mgr.GetCluster(ctx, req.ClusterName)
			if err != nil {
				return reconcile.Result{}, err
			}

			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(gvk)
			if err := cl.GetClient().Get(ctx, req.NamespacedName, obj); err != nil {
				return reconcile.Result{}, err
			}

			if !obj.GetDeletionTimestamp().IsZero() {
				// TODO handle deletion
				if slices.Contains(obj.GetFinalizers(), genericFinalizer) {
					// remove finalizer
					finalizers := slices.DeleteFunc(
						obj.GetFinalizers(),
						func(s string) bool {
							return s == genericFinalizer
						},
					)
					obj.SetFinalizers(finalizers)
					if err := cl.GetClient().Update(ctx, obj); err != nil {
						return mctrl.Result{}, err
					}
				}
				return mctrl.Result{}, nil
			}

			// add finalizer if not present
			if !slices.Contains(obj.GetFinalizers(), genericFinalizer) {
				finalizers := append(obj.GetFinalizers(), genericFinalizer)
				obj.SetFinalizers(finalizers)
				if err := cl.GetClient().Update(ctx, obj); err != nil {
					return mctrl.Result{}, err
				}
			}

			provider, ok := obj.GetAnnotations()[providerClusterAnn]
			if !ok || provider == "" {
				// No provider cluster annotated, find one

				// Determine GVR for the GVK
				mapper := cl.GetRESTMapper()
				mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
				if err != nil {
					return mctrl.Result{}, err
				}

				possibleProviders, ok := b.apiAccepters[mapping.Resource.String()]
				if !ok || len(possibleProviders) == 0 {
					log.Info("No clusters accept this GVR, skipping")
					// TODO condition
					return mctrl.Result{}, nil
				}

				log.Info("Finding accepting cluster from possible providers", "possibleProviders", possibleProviders)

				for _, possibleProvider := range possibleProviders {
					providerCluster, err := b.mgr.GetCluster(ctx, possibleProvider)
					if err != nil {
						log.Error(err, "Failed to get possible provider cluster", "cluster", possibleProvider)
						continue
					}

					acceptAPIs := &brokerv1alpha1.AcceptAPIList{}
					if err := providerCluster.GetClient().List(ctx, acceptAPIs); err != nil {
						log.Error(err, "Failed to get AcceptAPIs from cluster", "cluster", possibleProvider)
						continue
					}
					for _, acceptAPI := range acceptAPIs.Items {
						log.Info("Checking AcceptAPI from possible provider cluster", "cluster", possibleProvider, "acceptAPI", acceptAPI.Name, "gvr", acceptAPI.Spec.GVR.String())
						if acceptAPI.Spec.GVR.String() != mapping.Resource.String() {
							continue
						}
						if acceptAPI.Spec.AppliesTo(log, obj) {
							log.Info("Found accepting cluster", "cluster", possibleProvider)
							provider = possibleProvider
							break
						}
					}
					if provider != "" {
						// Because I don't like loop labels.
						break
					}
				}
				if provider == "" {
					log.Info("No accepting cluster found after filtering, skipping")
					// TODO condition
					return mctrl.Result{}, fmt.Errorf("no accepting cluster found for resource")
				}

				anns := obj.GetAnnotations()
				if anns == nil {
					anns = make(map[string]string)
				}
				anns[providerClusterAnn] = provider
				obj.SetAnnotations(anns)
				if err := cl.GetClient().Update(ctx, obj); err != nil {
					log.Error(err, "Failed to annotate resource with provider cluster")
					return mctrl.Result{}, err
				}
			}

			// TODO this is assuming that the annotated provider is
			// valid. if the provider is not valid a new provider should
			// be chosen.

			providerCluster, err := b.mgr.GetCluster(ctx, provider)
			if err != nil {
				log.Error(err, "Failed to get provider cluster", "cluster", provider)
				return mctrl.Result{}, err
			}

			log.Info("Syncing resource between consumer and provider cluster", "cluster", provider)
			// TODO send conditions back to consumer cluster
			_, err = CopyResource(
				ctx,
				gvk,
				req.NamespacedName,
				cl.GetClient(),
				providerCluster.GetClient(),
			)
			if err != nil {
				log.Error(err, "Failed to copy resource to provider cluster", "cluster", provider)
				return mctrl.Result{}, err
			}

			return mctrl.Result{}, nil
		},
	)
}
