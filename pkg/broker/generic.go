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
	"strings"

	"github.com/go-logr/logr"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

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

func (b *Broker) genericReconciler(name string, mgr mctrl.Manager, gvk schema.GroupVersionKind) error {
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
		Named(name + "-generic-" + gvk.String()).
		For(obj).
		Complete(&gr)
}

const (
	genericFinalizer   = "broker.platform-mesh.io/generic-finalizer"
	consumerClusterAnn = "broker.platform-mesh.io/consumer-cluster"
	providerClusterAnn = "broker.platform-mesh.io/provider-cluster"
)

type genericReconciler struct {
	log logr.Logger
	gvk schema.GroupVersionKind

	getCluster          func(context.Context, string) (cluster.Cluster, error)
	getPossibleProvider func(metav1.GroupVersionResource, *unstructured.Unstructured) (string, error)
	getAcceptedAPIs     func(string, metav1.GroupVersionResource) ([]*brokerv1alpha1.AcceptAPI, error)
}

//nolint:gocyclo // cyclomatic complexity is high, refactor when time allows
func (gr *genericReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (mctrl.Result, error) {
	log := ctrllog.FromContext(ctx).WithValues("consumer", req.ClusterName)
	log.Info("Reconciling generic resource")

	var consumerName, providerName string
	var consumerCluster, providerCluster cluster.Cluster

	switch {
	case strings.HasPrefix(req.ClusterName, ConsumerPrefix):
		// Request comes from consumer cluster
		consumerName = req.ClusterName
		cl, err := gr.getCluster(ctx, consumerName)
		if err != nil {
			return mctrl.Result{}, fmt.Errorf("failed to get consumer cluster %q: %w", consumerName, err)
		}
		consumerCluster = cl
	case strings.HasPrefix(req.ClusterName, ProviderPrefix):
		// Request comes from provider cluster
		providerName = req.ClusterName
		cl, err := gr.getCluster(ctx, providerName)
		if err != nil {
			return mctrl.Result{}, fmt.Errorf("failed to get provider cluster %q: %w", providerName, err)
		}
		providerCluster = cl
	default:
		log.Info("Request cluster name does not have consumer or provider prefix, skipping")
		return mctrl.Result{}, nil
	}

	if consumerName == "" {
		// Request came from provider cluster, grab consumer name from
		// annotation
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gr.gvk)
		if err := providerCluster.GetClient().Get(ctx, req.NamespacedName, obj); err != nil {
			if apierrors.IsNotFound(err) {
				return mctrl.Result{}, nil
			}
			return mctrl.Result{}, fmt.Errorf("failed to get resource from provider cluster %q: %w", providerName, err)
		}

		consumerNameAnn, ok := obj.GetAnnotations()[consumerClusterAnn]
		if !ok || consumerNameAnn == "" {
			log.Info("Resource in provider cluster missing consumer cluster annotation, skipping")
			return mctrl.Result{}, nil
		}
		consumerName = consumerNameAnn

		cl, err := gr.getCluster(ctx, consumerName)
		if err != nil {
			return mctrl.Result{}, fmt.Errorf("failed to get consumer cluster %q: %w", consumerName, err)
		}
		consumerCluster = cl
	}

	gvr, err := gr.getGVR(consumerCluster)
	if err != nil {
		log.Error(err, "Failed to determine GVR for resource")
		return mctrl.Result{}, err
	}

	if providerName == "" {
		// Request came from consumer cluster, grab provider name from
		// annotation
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gr.gvk)
		if err := consumerCluster.GetClient().Get(ctx, req.NamespacedName, obj); err != nil {
			if apierrors.IsNotFound(err) {
				return mctrl.Result{}, nil
			}
			return mctrl.Result{}, fmt.Errorf("failed to get resource from consumer cluster %q: %w", consumerName, err)
		}

		possibleProviderName, err := gr.getProviderName(gvr, obj)
		if err != nil || possibleProviderName == "" {
			log.Info("No provider cluster found for resource, skipping")
			return mctrl.Result{}, fmt.Errorf("no provider cluster found for resource in consumer cluster %q: %w", consumerName, err)
		}

		providerName = possibleProviderName

		cl, err := gr.getCluster(ctx, providerName)
		if err != nil {
			return mctrl.Result{}, fmt.Errorf("failed to get provider cluster %q: %w", providerName, err)
		}
		providerCluster = cl
	}

	// Now consumer and provider are both set.
	log = log.WithValues("consumer", consumerName, "provider", providerName)

	consumerObj := &unstructured.Unstructured{}
	consumerObj.SetGroupVersionKind(gr.gvk)
	if err := consumerCluster.GetClient().Get(ctx, req.NamespacedName, consumerObj); err != nil {
		if apierrors.IsNotFound(err) {
			return mctrl.Result{}, nil
		}
		return mctrl.Result{}, err
	}

	if !consumerObj.GetDeletionTimestamp().IsZero() {
		if err := gr.deleteInCluster(ctx, providerCluster, consumerObj); err != nil {
			return mctrl.Result{}, fmt.Errorf("failed to delete resource from provider cluster %q during consumer deletion: %w", providerName, err)
		}
		if controllerutil.RemoveFinalizer(consumerObj, genericFinalizer) {
			if err := consumerCluster.GetClient().Update(ctx, consumerObj); err != nil {
				return mctrl.Result{}, fmt.Errorf("failed to remove finalizer during consumer deletion: %w", err)
			}
		}
		return mctrl.Result{}, nil
	}

	if controllerutil.AddFinalizer(consumerObj, genericFinalizer) {
		if err := consumerCluster.GetClient().Update(ctx, consumerObj); err != nil {
			return mctrl.Result{}, err
		}
	}

	log.Info("Annotating resources in consumer cluster with provider cluster")
	if err := gr.setAnnotation(ctx, consumerCluster, consumerObj, providerClusterAnn, providerName); err != nil {
		return mctrl.Result{}, err
	}

	providerAccepts, err := gr.providerAcceptsObj(providerName, gvr, consumerObj)
	if err != nil {
		return mctrl.Result{}, err
	}

	if !providerAccepts {
		log.Info("Annotated provider cluster no longer applies, deleting from provider")
		if err := gr.deleteInCluster(ctx, providerCluster, consumerObj); err != nil {
			log.Error(err, "Failed to delete resource from provider cluster")
			return mctrl.Result{}, err
		}

		log.Info("Annotated provider cluster no longer applies, removing annotation")
		anns := consumerObj.GetAnnotations()
		delete(anns, providerClusterAnn)
		consumerObj.SetAnnotations(anns)
		if err := consumerCluster.GetClient().Update(ctx, consumerObj); err != nil {
			log.Error(err, "Failed to remove provider annotation from resource")
			return mctrl.Result{}, err
		}
		// TODO conditions
		return mctrl.Result{Requeue: true}, nil
	}

	log.Info("Syncing resource between consumer and provider cluster")
	// TODO send conditions back to consumer cluster
	// TODO there should be two informers triggering this - one
	// for consumer and one for provider
	_, err = CopyResource(
		ctx,
		gr.gvk,
		req.NamespacedName,
		consumerCluster.GetClient(),
		providerCluster.GetClient(),
	)
	if err != nil {
		log.Error(err, "Failed to copy resource to provider cluster")
		return mctrl.Result{}, err
	}

	log.Info("Getting synced resource from provider cluster")
	providerObj := &unstructured.Unstructured{}
	providerObj.SetGroupVersionKind(gr.gvk)
	if err := providerCluster.GetClient().Get(ctx, req.NamespacedName, providerObj); err != nil {
		log.Error(err, "Failed to get synced resource from provider cluster")
		return mctrl.Result{}, err
	}

	log.Info("Annotating resource in provider cluster with consumer cluster")
	if err := gr.setAnnotation(ctx, providerCluster, providerObj, consumerClusterAnn, consumerName); err != nil {
		return mctrl.Result{}, err
	}

	// TODO handle resource drift when a related resource is removed in
	// the provider it needs to be removed in the consumer
	// maybe just a finalizer on the resources in the provider?
	relatedResourcesI, found, err := unstructured.NestedSlice(providerObj.Object, "status", "relatedResources")
	if err != nil {
		log.Error(err, "Failed to get related resources from synced resource status")
		return mctrl.Result{}, err
	}
	if !found {
		log.Info("No related resources found in synced resource status")
		return mctrl.Result{}, nil
	}

	relatedResources := make([]brokerv1alpha1.RelatedResource, 0, len(relatedResourcesI))
	for _, rrI := range relatedResourcesI {
		rrMap, ok := rrI.(map[string]interface{})
		if !ok {
			return mctrl.Result{}, fmt.Errorf("failed to cast related resource from synced resource status")
		}

		var rr brokerv1alpha1.RelatedResource
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(rrMap, &rr); err != nil {
			return mctrl.Result{}, fmt.Errorf("failed to convert related resource from synced resource status: %w", err)
		}

		relatedResources = append(relatedResources, rr)
	}

	log.Info("Syncing related resources from provider to consumer", "count", len(relatedResources))

	for _, relatedResource := range relatedResources {
		log.Info("Syncing related resource", "relatedResource", relatedResource)
		providerRRObj := &unstructured.Unstructured{}
		providerRRObj.SetGroupVersionKind(relatedResource.SchemaGVK())
		if err := providerCluster.GetClient().Get(ctx, client.ObjectKey{
			Namespace: relatedResource.Namespace,
			Name:      relatedResource.Name,
		}, providerRRObj); err != nil {
			log.Error(err, "Failed to get related resource from provider cluster", "relatedResource", relatedResource)
			continue
		}

		// TODO conditions
		_, err := CopyResource(
			ctx,
			relatedResource.SchemaGVK(),
			types.NamespacedName{
				Namespace: relatedResource.Namespace, // TODO namespace from consumer?
				Name:      relatedResource.Name,
			},
			providerCluster.GetClient(),
			consumerCluster.GetClient(),
		)
		if err != nil {
			log.Error(err, "Failed to copy owned resource to provider cluster")
			continue
		}

		log.Info("Getting synced resource from consumer cluster")
		consumerRRObj := &unstructured.Unstructured{}
		consumerRRObj.SetGroupVersionKind(relatedResource.SchemaGVK())
		if err := consumerCluster.GetClient().Get(ctx, client.ObjectKey{
			Namespace: relatedResource.Namespace,
			Name:      relatedResource.Name,
		}, consumerRRObj); err != nil {
			log.Error(err, "Failed to get synced owned resource from consumer cluster")
			continue
		}

		log.Info("Setting owner reference on related resource in consumer cluster")
		if err := controllerutil.SetOwnerReference(consumerObj, consumerRRObj, consumerCluster.GetScheme()); err != nil {
			log.Error(err, "Failed to set owner reference on owned resource in consumer cluster")
			continue
		}
		if err := consumerCluster.GetClient().Update(ctx, consumerRRObj); err != nil {
			log.Error(err, "Failed to set owner reference on owned resource in consumer cluster")
			continue
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

func (gr *genericReconciler) getGVR(cl cluster.Cluster) (metav1.GroupVersionResource, error) {
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

func (gr *genericReconciler) providerAcceptsObj(providerName string, gvr metav1.GroupVersionResource, obj *unstructured.Unstructured) (bool, error) {
	acceptAPIs, err := gr.getAcceptedAPIs(providerName, gvr)
	if err != nil {
		return false, err
	}
	for _, acceptAPI := range acceptAPIs {
		if acceptAPI.AppliesTo(gvr, obj) {
			return true, nil
		}
	}
	return false, nil
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
