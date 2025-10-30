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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
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
	return mcbuilder.ControllerManagedBy(mgr).
		Named("generic-" + gvk.String()).
		For(obj).
		Complete(b.genericReconcilerFactory(gvk))
}

const (
	genericFinalizer   = "broker.platform-mesh.io/generic-finalizer"
	providerClusterAnn = "broker.platform-mesh.io/provider-cluster"
)

// TODO split into a proper reconciler struct
//
//nolint:gocyclo
func (b *Broker) genericReconcilerFactory(gvk schema.GroupVersionKind) reconcile.TypedReconciler[mcreconcile.Request] {
	return mcreconcile.Func(
		func(ctx context.Context, req mctrl.Request) (mctrl.Result, error) {
			log := ctrllog.FromContext(ctx).WithValues("cluster", req.ClusterName)
			log.Info("Reconciling generic resource", "gvk", gvk)

			cl, err := b.mgr.GetCluster(ctx, req.ClusterName)
			if err != nil {
				return mctrl.Result{}, err
			}

			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(gvk)
			if err := cl.GetClient().Get(ctx, req.NamespacedName, obj); err != nil {
				if apierrors.IsNotFound(err) {
					return mctrl.Result{}, nil
				}
				return mctrl.Result{}, err
			}

			if !obj.GetDeletionTimestamp().IsZero() {
				// Delete from provider cluster if annotated
				if provider, ok := obj.GetAnnotations()[providerClusterAnn]; ok && provider != "" {
					if providerCluster, err := b.mgr.GetCluster(ctx, provider); err == nil {
						if err := providerCluster.GetClient().Delete(ctx, StripClusterMetadata(obj)); err != nil {
							if !apierrors.IsNotFound(err) {
								log.Error(err, "Failed to delete resource from provider cluster during finalization", "cluster", provider)
								return mctrl.Result{}, err
							}
						}
					}
				}

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

			// Determine GVR for the GVK
			mapper := cl.GetRESTMapper()
			mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
			if err != nil {
				return mctrl.Result{}, err
			}

			// mapper returns schema.GroupVersionResource, broker works
			// with metav1.GroupVersionResource
			gvr := metav1.GroupVersionResource{
				Group:    mapping.Resource.Group,
				Version:  mapping.Resource.Version,
				Resource: mapping.Resource.Resource,
			}

			provider, ok := obj.GetAnnotations()[providerClusterAnn]
			if !ok || provider == "" {
				// No provider cluster annotated, find one

				b.lock.RLock()
				possibleProviders, ok := b.apiAccepters[gvr]
				b.lock.RUnlock()
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

					_, applies, err := providerApplies(ctx, providerCluster, gvr, obj)
					if err != nil {
						log.Error(err, "Failed to check if provider applies", "cluster", possibleProvider)
						continue
					}

					if applies {
						log.Info("Found accepting cluster", "cluster", possibleProvider)
						provider = possibleProvider
						break
					}
				}
				if provider == "" {
					log.Info("No accepting cluster found after filtering, skipping")
					// TODO condition
					return mctrl.Result{}, nil
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

			providerCluster, err := b.mgr.GetCluster(ctx, provider)
			if err != nil {
				log.Error(err, "Failed to get provider cluster", "cluster", provider)
				// TODO should probably choose a new provider here
				// and might need a cleanup if a provider was offline
				// for a time and still has resources that then have new
				// providers
				return mctrl.Result{}, err
			}

			// Verify that the provider still applies
			acceptAPI, applies, err := providerApplies(ctx, providerCluster, gvr, obj)
			if err != nil {
				log.Error(err, "Failed to check if provider still applies", "cluster", provider)
				return mctrl.Result{}, err
			}
			if !applies {
				log.Info("Provider cluster no longer applies, deleting in provider", "cluster", provider)
				if err := providerCluster.GetClient().Delete(ctx, StripClusterMetadata(obj)); err != nil {
					if !apierrors.IsNotFound(err) {
						log.Error(err, "Failed to delete resource from provider cluster", "cluster", provider)
						return mctrl.Result{}, err
					}
				}

				anns := obj.GetAnnotations()
				delete(anns, providerClusterAnn)
				obj.SetAnnotations(anns)
				if err := cl.GetClient().Update(ctx, obj); err != nil {
					log.Error(err, "Failed to remove provider annotation from resource")
					return mctrl.Result{}, err
				}
				// TODO conditions
				return mctrl.Result{}, nil
			}

			log.Info("Syncing resource between consumer and provider cluster", "cluster", provider)
			// TODO send conditions back to consumer cluster
			// TODO there should be two informers triggering this - one
			// for consumer and one for provider
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

			for _, relatedGVR := range acceptAPI.Spec.RelatedResources {
				objs := &unstructured.UnstructuredList{}
				if err := providerCluster.GetClient().List(
					ctx,
					objs,
					client.InNamespace(req.Namespace),
					client.MatchingLabels{
						brokerv1alpha1.RelatedResourceLabel: req.Name,
					},
				); err != nil {
					log.Error(err, "Failed to list related resources from provider cluster",
						"relatedGVR", relatedGVR, "cluster", provider)
					continue
				}

				// TODO no drift detection atm. this should look for
				// orphaned resources in the consumer cluster and delete
				// them
				for _, relatedObj := range objs.Items {
					log = log.WithValues("relatedGVR", relatedGVR, "relatedName", relatedObj.GetName())
					log.Info("Syncing related resource from provider to consumer")
					// TODO conditions
					_, err := CopyResource(
						ctx,
						relatedObj.GroupVersionKind(),
						client.ObjectKey{Namespace: relatedObj.GetNamespace(), Name: relatedObj.GetName()},
						providerCluster.GetClient(),
						cl.GetClient(),
					)
					if err != nil {
						log.Error(err, "Failed to copy related resource to consumer cluster")
					}
				}
			}

			return mctrl.Result{}, nil
		},
	)
}

func providerApplies(
	ctx context.Context,
	cl cluster.Cluster,
	gvr metav1.GroupVersionResource,
	obj *unstructured.Unstructured,
) (*brokerv1alpha1.AcceptAPI, bool, error) {
	// TODO cache AcceptAPI, the acceptApi reconciler can update a cache
	// in the broker
	acceptAPIs := &brokerv1alpha1.AcceptAPIList{}
	if err := cl.GetClient().List(ctx, acceptAPIs); err != nil {
		return nil, false, fmt.Errorf("failed to list AcceptAPIs: %w", err)
	}

	for _, acceptAPI := range acceptAPIs.Items {
		if acceptAPI.Spec.GVR.String() != gvr.String() {
			continue
		}
		if acceptAPI.Spec.AppliesTo(obj) {
			return &acceptAPI, true, nil
		}
	}

	return nil, false, nil
}
