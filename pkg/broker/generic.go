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
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/go-logr/logr"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	mctrl "sigs.k8s.io/multicluster-runtime"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
)

func (b *Broker) genericReconciler(mgr mctrl.Manager, gvk schema.GroupVersionKind) error {
	gr := genericReconciler{
		log:        ctrllog.Log.WithName("generic-reconciler").WithValues("gvk", gvk),
		gvk:        gvk,
		getCluster: b.mgr.GetCluster,
		getAcceptedAPIs: func(providerName string, gvr metav1.GroupVersionResource) ([]*brokerv1alpha1.AcceptAPI, error) {
			b.lock.RLock()
			defer b.lock.RUnlock()
			acceptAPIs, ok := b.apiAccepters[gvr][providerName]
			if !ok {
				return nil, fmt.Errorf("provider %q does not accept %q", providerName, gvr)
			}
			return slices.Collect(maps.Values(acceptAPIs)), nil
		},
		getPossibleProvider: func(gvr metav1.GroupVersionResource, obj *unstructured.Unstructured) (string, error) {
			b.lock.RLock()
			defer b.lock.RUnlock()
			possibleProviders, ok := b.apiAccepters[gvr]

			if !ok || len(possibleProviders) == 0 {
				return "", fmt.Errorf("no clusters accept GVR %v", gvr)
			}

			for possibleProvider, acceptedAPIs := range possibleProviders {
				for _, acceptAPI := range acceptedAPIs {
					if acceptAPI.AppliesTo(gvr, obj) {
						return possibleProvider, nil
					}
				}
			}

			return "", fmt.Errorf("no accepting cluster found for GVR %v", gvr)
		},
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	return mcbuilder.ControllerManagedBy(mgr).
		Named("generic-" + gvk.String()).
		For(obj).
		Complete(&gr)
}

const (
	genericFinalizer   = "broker.platform-mesh.io/generic-finalizer"
	providerClusterAnn = "broker.platform-mesh.io/provider-cluster"
)

type genericReconciler struct {
	log logr.Logger
	gvk schema.GroupVersionKind

	getCluster          func(ctx context.Context, name string) (cluster.Cluster, error)
	getPossibleProvider func(gvr metav1.GroupVersionResource, obj *unstructured.Unstructured) (string, error)
	getAcceptedAPIs     func(providerName string, gvr metav1.GroupVersionResource) ([]*brokerv1alpha1.AcceptAPI, error)
}

func (gr *genericReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (mctrl.Result, error) {
	log := ctrllog.FromContext(ctx).WithValues("cluster", req.ClusterName)
	log.Info("Reconciling generic resource")

	cl, err := gr.getCluster(ctx, req.ClusterName)
	if err != nil {
		return mctrl.Result{}, err
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gr.gvk)
	if err := cl.GetClient().Get(ctx, req.NamespacedName, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return mctrl.Result{}, nil
		}
		return mctrl.Result{}, err
	}

	if !obj.GetDeletionTimestamp().IsZero() {
		return gr.ReconcileDelete(ctx, cl, obj)
	}

	// add finalizer if not present
	if controllerutil.AddFinalizer(obj, genericFinalizer) {
		if err := cl.GetClient().Update(ctx, obj); err != nil {
			return mctrl.Result{}, err
		}
	}

	gvr, err := gr.GetGVR(cl)
	if err != nil {
		log.Error(err, "Failed to determine GVR for resource")
		return mctrl.Result{}, err
	}

	providerName, err := gr.getProviderName(gvr, obj)
	if err != nil {
		log.Error(err, "Failed to determine provider cluster for resource")
		// TODO conditions
		return mctrl.Result{}, err
	}

	log.Info("Annotating resource with provider cluster", "cluster", providerName)
	if err := gr.setAnnotation(ctx, cl, obj, providerClusterAnn, providerName); err != nil {
		return mctrl.Result{}, err
	}

	providerCluster, err := gr.getCluster(ctx, providerName)
	if err != nil {
		log.Error(err, "Failed to get provider cluster", "cluster", providerName)
		// TODO should probably choose a new provider here
		// and might need a cleanup if a provider was offline
		// for a time and still has resources that then have new
		// providers
		return mctrl.Result{}, err
	}

	acceptedProviderAPIs, err := gr.getAcceptedAPIs(providerName, gvr)
	if err != nil {
		log.Info("Annotated provider cluster no longer accepts this GVR, removing annotation", "cluster", providerName)
		if err := gr.deleteInCluster(ctx, providerCluster, obj); err != nil {
			log.Error(err, "Failed to delete resource from provider cluster", "cluster", providerName)
			return mctrl.Result{}, err
		}
		anns := obj.GetAnnotations()
		delete(anns, providerClusterAnn)
		obj.SetAnnotations(anns)
		if err := cl.GetClient().Update(ctx, obj); err != nil {
			log.Error(err, "Failed to remove provider annotation from resource")
			return mctrl.Result{}, err
		}
		// TODO conditions
		return mctrl.Result{Requeue: true}, nil
	}

	var acceptAPI *brokerv1alpha1.AcceptAPI
	for i := range acceptedProviderAPIs {
		if acceptedProviderAPIs[i].AppliesTo(gvr, obj) {
			acceptAPI = acceptedProviderAPIs[i]
			break
		}
	}
	if acceptAPI == nil {
		log.Info("Annotated provider cluster no longer applies, deleting from provider", "cluster", providerName)
		if err := gr.deleteInCluster(ctx, providerCluster, obj); err != nil {
			log.Error(err, "Failed to delete resource from provider cluster", "cluster", providerName)
			return mctrl.Result{}, err
		}

		log.Info("Annotated provider cluster no longer applies, removing annotation", "cluster", providerName)
		anns := obj.GetAnnotations()
		delete(anns, providerClusterAnn)
		obj.SetAnnotations(anns)
		if err := cl.GetClient().Update(ctx, obj); err != nil {
			log.Error(err, "Failed to remove provider annotation from resource")
			return mctrl.Result{}, err
		}
		// TODO conditions
		return mctrl.Result{Requeue: true}, nil
	}

	log.Info("Syncing resource between consumer and provider cluster", "cluster", providerName)
	// TODO send conditions back to consumer cluster
	// TODO there should be two informers triggering this - one
	// for consumer and one for provider
	_, err = CopyResource(
		ctx,
		gr.gvk,
		req.NamespacedName,
		cl.GetClient(),
		providerCluster.GetClient(),
	)
	if err != nil {
		log.Error(err, "Failed to copy resource to provider cluster", "cluster", providerName)
		return mctrl.Result{}, err
	}

	syncedObj := &unstructured.Unstructured{}
	syncedObj.SetGroupVersionKind(gr.gvk)
	if err := providerCluster.GetClient().Get(ctx, req.NamespacedName, syncedObj); err != nil {
		log.Error(err, "Failed to get synced resource from provider cluster", "cluster", providerName)
		return mctrl.Result{}, err
	}

	// TODO this is volatile, related resources should be
	// defined through another kind instead of part of the
	// AcceptAPI. ATM it is possible fwo two AcceptAPIs with
	// differing RelatedResources to accept the same
	// API/resource.
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
				"relatedGVR", relatedGVR, "cluster", providerName)
			continue
		}

		// TODO no drift detection atm. this should look for
		// orphaned resources in the consumer cluster and delete
		// them
		for _, relatedObj := range objs.Items {
			log := log.WithValues("relatedGVR", relatedGVR, "relatedName", relatedObj.GetName())
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
				continue
			}

			// set owner reference to the synced object
			if err := controllerutil.SetOwnerReference(syncedObj, &relatedObj, providerCluster.GetScheme()); err != nil {
				log.Error(err, "Failed to set owner reference on related resource in provider cluster")
				continue
			}
			if err := providerCluster.GetClient().Update(ctx, &relatedObj); err != nil {
				log.Error(err, "Failed to set owner reference on related resource in provider cluster")
				continue
			}
		}
	}

	return mctrl.Result{}, nil
}

func (gr *genericReconciler) deleteInCluster(ctx context.Context, cl cluster.Cluster, obj *unstructured.Unstructured) error {
	if err := cl.GetClient().Delete(ctx, StripClusterMetadata(obj)); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (gr *genericReconciler) setAnnotation(ctx context.Context, cl cluster.Cluster, obj *unstructured.Unstructured, key, value string) error {
	anns := obj.GetAnnotations()
	if anns[key] == value {
		return nil
	}
	if anns == nil {
		anns = make(map[string]string)
	}
	anns[key] = value
	obj.SetAnnotations(anns)
	if err := cl.GetClient().Update(ctx, obj); err != nil {
		return err
	}
	return nil
}

func (gr *genericReconciler) getProviderName(gvr metav1.GroupVersionResource, obj *unstructured.Unstructured) (string, error) {
	providerName, ok := obj.GetAnnotations()[providerClusterAnn]
	if ok && providerName != "" {
		return providerName, nil
	}

	return gr.getPossibleProvider(gvr, obj)
}

func (gr *genericReconciler) GetGVR(cl cluster.Cluster) (metav1.GroupVersionResource, error) {
	mapper := cl.GetRESTMapper()
	mapping, err := mapper.RESTMapping(gr.gvk.GroupKind(), gr.gvk.Version)
	if err != nil {
		return metav1.GroupVersionResource{}, err
	}

	// mapper returns schema.GroupVersionResource, broker works
	// with metav1.GroupVersionResource
	return metav1.GroupVersionResource{
		Group:    mapping.Resource.Group,
		Version:  mapping.Resource.Version,
		Resource: mapping.Resource.Resource,
	}, nil
}

func (gr *genericReconciler) ReconcileDelete(
	ctx context.Context,
	cl cluster.Cluster,
	obj *unstructured.Unstructured,
) (mctrl.Result, error) {
	// Delete from provider cluster if annotated
	if provider, ok := obj.GetAnnotations()[providerClusterAnn]; ok && provider != "" {
		providerCluster, err := gr.getCluster(ctx, provider)
		if err != nil && !errors.Is(err, multicluster.ErrClusterNotFound) {
			return mctrl.Result{}, fmt.Errorf("failed to get provider cluster %q during finalization: %w", provider, err)
		}
		if err == nil {
			if err := gr.deleteInCluster(ctx, providerCluster, obj); err != nil {
				return mctrl.Result{}, fmt.Errorf("failed to delete resource from provider cluster %q during finalization: %w", provider, err)
			}
		}
	}

	if controllerutil.RemoveFinalizer(obj, genericFinalizer) {
		if err := cl.GetClient().Update(ctx, obj); err != nil {
			return mctrl.Result{}, fmt.Errorf("failed to remove finalizer during finalization: %w", err)
		}
	}
	return mctrl.Result{}, nil
}
