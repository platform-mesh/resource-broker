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

package generic

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	mctrl "sigs.k8s.io/multicluster-runtime"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
	"github.com/platform-mesh/resource-broker/pkg/kubernetes"
	"github.com/platform-mesh/resource-broker/pkg/sync"
)

// Options are the options for the generic reconciler.
type Options struct {
	Coordination              client.Client
	GetProviderCluster        func(context.Context, string) (cluster.Cluster, error)
	GetConsumerCluster        func(context.Context, string) (cluster.Cluster, error)
	GetProviders              func(metav1.GroupVersionResource) map[string]map[string]brokerv1alpha1.AcceptAPI
	GetProviderAcceptedAPIs   func(string, metav1.GroupVersionResource) ([]brokerv1alpha1.AcceptAPI, error)
	GetMigrationConfiguration func(metav1.GroupVersionKind, metav1.GroupVersionKind) (brokerv1alpha1.MigrationConfiguration, bool)
}

// ReconcileFunc returns a reconciler function for generic resources.
func ReconcileFunc(opts Options, gvk schema.GroupVersionKind) mcreconcile.Func {
	return func(ctx context.Context, req mctrl.Request) (mctrl.Result, error) {
		gr := &genericReconciler{
			opts: opts,
			log:  ctrllog.FromContext(ctx).WithValues("clusterName", req.ClusterName),
			gvk:  gvk,
			req:  req,
		}
		return gr.reconcile(ctx)
	}
}

const (
	genericFinalizer      = "broker.platform-mesh.io/generic-finalizer"
	consumerClusterAnn    = "broker.platform-mesh.io/consumer-cluster"
	providerClusterAnn    = "broker.platform-mesh.io/provider-cluster"
	newProviderClusterAnn = "broker.platform-mesh.io/new-provider-cluster"
)

type genericReconciler struct {
	opts Options
	log  logr.Logger
	gvk  schema.GroupVersionKind
	req  mctrl.Request

	consumerName       string
	consumerCluster    cluster.Cluster
	providerName       string
	providerCluster    cluster.Cluster
	newProviderName    string
	newProviderCluster cluster.Cluster

	gvr metav1.GroupVersionResource
}

func (gr *genericReconciler) reconcile(ctx context.Context) (mctrl.Result, error) {
	gr.log.Info("Reconciling generic resource")

	cont, err := gr.determineClusters(ctx)
	if err != nil {
		gr.log.Error(err, "Failed to determine clusters")
		return mctrl.Result{}, err
	}
	if !cont {
		// Not continuing
		return mctrl.Result{}, nil
	}

	if gr.consumerCluster == nil {
		gr.log.Info("Consumer cluster is not set, skipping")
		return mctrl.Result{}, gr.deleteObjs(ctx)
	}

	gr.log = gr.log.WithValues("consumer", gr.consumerName, "provider", gr.providerName)

	consumerObj, err := gr.getConsumerObj(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return mctrl.Result{}, gr.deleteObjs(ctx)
		}
		return mctrl.Result{}, fmt.Errorf("failed to get resource from consumer cluster %q: %w", gr.consumerName, err)
	}

	if !consumerObj.GetDeletionTimestamp().IsZero() {
		gr.log.Info("Resource in consumer cluster is being deleted, finalizing")
		return mctrl.Result{}, gr.deleteObjs(ctx)
	}

	if err := gr.decorateInConsumer(ctx); err != nil {
		return mctrl.Result{}, err
	}

	providerAccepts := true
	// Only check provider acceptance if there isn't already a migration
	// going on.
	if gr.newProviderCluster == nil {
		var err error
		providerAccepts, err = gr.providerAcceptsObj(ctx)
		if err != nil {
			return mctrl.Result{}, err
		}
	}

	if gr.newProviderCluster != nil || !providerAccepts {
		gr.log.Info("Provider no longer accepts resource")
		if err := gr.newProvider(ctx, consumerObj); err != nil {
			return mctrl.Result{}, err
		}

		status, found, err := gr.getNewProviderStatus(ctx)
		if err != nil {
			return mctrl.Result{}, err
		}
		if found && !status.Continue() {
			return mctrl.Result{}, nil
		}

		cont, state, err := gr.migrate(ctx, consumerObj)
		if err != nil {
			gr.log.Error(err, "Failed to check migration status")
			return mctrl.Result{}, err
		}
		if !cont {
			gr.log.Info("Migration not yet ready to continue, waiting")
			return mctrl.Result{}, nil
		}

		// Copy related resources from new provider when the cutover
		// to the new provider can start.
		switch state {
		case brokerv1alpha1.MigrationStateInitialCompleted, brokerv1alpha1.MigrationStateCutoverInProgress, brokerv1alpha1.MigrationStateCutoverCompleted:
			gr.log.Info("Syncing related resources from new provider")
			if err := gr.syncRelatedResources(ctx, gr.newProviderName, gr.newProviderCluster); err != nil {
				return mctrl.Result{}, err
			}
		}

		if state != brokerv1alpha1.MigrationStateCutoverCompleted {
			gr.log.Info("Migration not yet completed, waiting")
			return mctrl.Result{}, nil
		}

		gr.log.Info("Deleting from old provider")
		if err := gr.deleteObj(ctx, gr.providerCluster); err != nil {
			return mctrl.Result{}, fmt.Errorf("failed to delete resource from old provider cluster %q: %w", gr.providerName, err)
		}

		gr.providerName = gr.newProviderName
		gr.providerCluster = gr.newProviderCluster
		gr.newProviderName = ""
		gr.newProviderCluster = nil

		return mctrl.Result{Requeue: true}, gr.decorateInConsumer(ctx)
	}

	if err := gr.syncResource(ctx, gr.providerName, gr.providerCluster); err != nil {
		return mctrl.Result{}, err
	}

	status, found, err := gr.getProviderStatus(ctx)
	if err != nil {
		gr.log.Error(err, "Failed to get provider status")
		return mctrl.Result{}, err
	}
	if found && !status.Continue() {
		gr.log.Info("Provider status indicates to not continue")
		return mctrl.Result{}, nil
	}

	if err := gr.syncRelatedResources(ctx, gr.providerName, gr.providerCluster); err != nil {
		return mctrl.Result{}, err
	}

	return mctrl.Result{}, nil
}

func (gr *genericReconciler) getPossibleProvider(obj *unstructured.Unstructured) (string, error) {
	possibleProviders := gr.opts.GetProviders(gr.gvr)
	if len(possibleProviders) == 0 {
		return "", fmt.Errorf("no clusters accept GVR %v", gr.gvr)
	}

	for possibleProvider, acceptedAPIs := range possibleProviders {
		for _, acceptAPI := range acceptedAPIs {
			applies, reasons := acceptAPI.AppliesTo(gr.gvr, obj)
			if applies {
				return possibleProvider, nil
			}
			gr.log.Info("Provider does not accept resource due to filter mismatch", "provider", possibleProvider, "reasons", reasons)
		}
	}

	return "", fmt.Errorf("no accepting cluster found for GVR %v", gr.gvr)
}

func (gr *genericReconciler) getProviderName(obj *unstructured.Unstructured) (string, error) {
	providerName, ok := obj.GetAnnotations()[providerClusterAnn]
	if ok && providerName != "" {
		gr.log.Info("Found provider cluster annotation", "provider", providerName)
		return providerName, nil
	}

	gr.log.Info("Found no provider in annotations, looking for possible providers")
	return gr.getPossibleProvider(obj)
}

func (gr *genericReconciler) getNewProviderName(obj *unstructured.Unstructured) (string, error) {
	providerName, ok := obj.GetAnnotations()[newProviderClusterAnn]
	if ok && providerName != "" {
		gr.log.Info("Found new provider cluster annotation", "newProvider", providerName)
		return providerName, nil
	}

	return gr.getPossibleProvider(obj)
}

func (gr *genericReconciler) determineClusters(ctx context.Context) (bool, error) {
	// This made more sense before the logic was moved into its own
	// package. Should refactor this when time permits.

	if _, err := gr.opts.GetConsumerCluster(ctx, gr.req.ClusterName); err == nil {
		gr.log.Info("Request comes from consumer cluster")
		if err := gr.setConsumerCluster(ctx, gr.req.ClusterName); err != nil {
			gr.log.Error(err, "Failed to set consumer cluster")
			return false, err
		}
	}

	if _, err := gr.opts.GetProviderCluster(ctx, gr.req.ClusterName); err == nil {
		gr.log.Info("Request comes from provider cluster")
		if err := gr.setProviderCluster(ctx, gr.req.ClusterName); err != nil {
			gr.log.Error(err, "Failed to set provider cluster")
			return false, err
		}
	}

	if gr.consumerName == "" && gr.providerName == "" {
		gr.log.Info("Request does not come from known consumer or provider cluster, skipping")
		return false, nil
	}

	if gr.consumerName == "" {
		if err := gr.setConsumerClusterFromProvider(ctx); err != nil {
			return false, err
		}
		// If consumer cluster is still nil the request cannot be
		// served. Cause might be that the same resource exists in
		// a provider cluster but doesn't originate from the broker.
		if gr.consumerCluster == nil {
			return false, nil
		}
	}

	// GVR must be set here because it is needed to find possible
	// providers based on accepted APIs
	var err error
	gr.gvr, err = gr.getGVR()
	if err != nil {
		gr.log.Error(err, "Failed to determine GVR for resource")
		return false, err
	}

	if gr.providerName == "" {
		if err := gr.setProviderClusterFromConsumer(ctx); err != nil {
			return false, err
		}
	}

	// Do a sanity check so an event from the new provider cluster does
	// not start overwriting things from the new provider cluster before
	// the migration is done.
	consumerObj, err := gr.getConsumerObj(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Consumer object not found, nothing to do
			return false, nil
		}
		return false, fmt.Errorf("failed to get resource from consumer cluster %q: %w", gr.consumerName, err)
	}

	var ok bool
	gr.newProviderName, ok = consumerObj.GetAnnotations()[newProviderClusterAnn]
	if !ok {
		// No new provider annotation, continue
		return true, nil
	}
	gr.log.Info("Found new provider annotation", "newProvider", gr.newProviderName)

	if gr.req.ClusterName == gr.newProviderName {
		gr.log.Info("Event comes from new provider cluster")
		gr.newProviderCluster = gr.providerCluster
		if err := gr.setProviderCluster(ctx, consumerObj.GetAnnotations()[providerClusterAnn]); err != nil {
			return false, err
		}
		return true, nil
	}

	gr.newProviderCluster, err = gr.opts.GetProviderCluster(ctx, gr.newProviderName)
	if err != nil {
		return false, fmt.Errorf("failed to get new provider cluster %q: %w", gr.newProviderName, err)
	}

	return true, nil
}

func (gr *genericReconciler) setConsumerCluster(ctx context.Context, name string) error {
	gr.log.Info("Setting consumer cluster", "consumer", name)
	cl, err := gr.opts.GetConsumerCluster(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to get consumer cluster %q: %w", name, err)
	}
	gr.consumerName = name
	gr.consumerCluster = cl
	return nil
}

func (gr *genericReconciler) setConsumerClusterFromProvider(ctx context.Context) error {
	gr.log.Info("Determining consumer cluster from provider annotation")
	providerObj, err := gr.getProviderObj(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the provider object is not found, the consumer cluster
			// cannot be set based on its annotation.
			return nil
		}
		return fmt.Errorf("failed to get resource from provider cluster %q: %w", gr.providerName, err)
	}

	consumerNameAnn, ok := providerObj.GetAnnotations()[consumerClusterAnn]
	if !ok || consumerNameAnn == "" {
		gr.log.Info("Resource in provider cluster missing consumer cluster annotation, skipping")
		return nil
	}

	gr.log.Info("Found consumer cluster annotation in provider", "consumer", consumerNameAnn)
	return gr.setConsumerCluster(ctx, consumerNameAnn)
}

func (gr *genericReconciler) setProviderCluster(ctx context.Context, name string) error {
	gr.log.Info("Setting provider cluster", "provider", name)
	cl, err := gr.opts.GetProviderCluster(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to get provider cluster %q: %w", name, err)
	}
	gr.providerName = name
	gr.providerCluster = cl
	return nil
}

func (gr *genericReconciler) setProviderClusterFromConsumer(ctx context.Context) error {
	gr.log.Info("Determining provider cluster from consumer annotation")
	consumerObj, err := gr.getConsumerObj(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the consumer object is not found, the provider cluster
			// cannot be set based on its annotation.
			return nil
		}
		return fmt.Errorf("failed to get resource from consumer cluster %q: %w", gr.consumerName, err)
	}

	possibleProviderName, err := gr.getProviderName(consumerObj)
	if err != nil {
		return fmt.Errorf("failed to determine provider cluster: %w", err)
	}
	if possibleProviderName == "" {
		return fmt.Errorf("no present or possible provider cluster found %q: %w", gr.consumerName, err)
	}

	gr.log.Info("Determined provider cluster", "provider", possibleProviderName)
	return gr.setProviderCluster(ctx, possibleProviderName)
}

func (gr *genericReconciler) getGVR() (metav1.GroupVersionResource, error) {
	if gr.consumerCluster == nil {
		return metav1.GroupVersionResource{}, fmt.Errorf("consumer cluster is not set")
	}
	mapper := gr.consumerCluster.GetRESTMapper()
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

func (gr *genericReconciler) getConsumerObj(ctx context.Context) (*unstructured.Unstructured, error) {
	consumerObj := &unstructured.Unstructured{}
	consumerObj.SetGroupVersionKind(gr.gvk)
	if err := gr.consumerCluster.GetClient().Get(ctx, gr.req.NamespacedName, consumerObj); err != nil {
		return nil, err
	}
	return consumerObj, nil
}

func (gr *genericReconciler) getProviderObj(ctx context.Context) (*unstructured.Unstructured, error) {
	providerObj := &unstructured.Unstructured{}
	providerObj.SetGroupVersionKind(gr.gvk)
	if err := gr.providerCluster.GetClient().Get(ctx, gr.req.NamespacedName, providerObj); err != nil {
		return nil, err
	}
	return providerObj, nil
}

func (gr *genericReconciler) getNewProviderObj(ctx context.Context) (*unstructured.Unstructured, error) {
	newProviderObj := &unstructured.Unstructured{}
	newProviderObj.SetGroupVersionKind(gr.gvk)
	if err := gr.newProviderCluster.GetClient().Get(ctx, gr.req.NamespacedName, newProviderObj); err != nil {
		return nil, err
	}
	return newProviderObj, nil
}

func (gr *genericReconciler) deleteObjs(ctx context.Context) error {
	if gr.providerCluster != nil {
		if err := gr.deleteObj(ctx, gr.providerCluster); err != nil {
			return fmt.Errorf("failed to delete resource from provider cluster %q: %w", gr.providerName, err)
		}
	}

	if gr.consumerCluster != nil {
		if err := gr.deleteObj(ctx, gr.consumerCluster); err != nil {
			return fmt.Errorf("failed to delete resource from consumer cluster %q: %w", gr.consumerName, err)
		}
	}

	return nil
}

func (gr *genericReconciler) deleteObj(ctx context.Context, cl cluster.Cluster) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gr.gvk)
	if err := cl.GetClient().Get(ctx, gr.req.NamespacedName, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if controllerutil.RemoveFinalizer(obj, genericFinalizer) {
		if err := cl.GetClient().Update(ctx, obj); err != nil {
			return fmt.Errorf("failed to remove finalizer during finalization: %w", err)
		}
	}

	if err := cl.GetClient().Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete resource during finalization: %w", err)
	}

	return nil
}

func (gr *genericReconciler) decorateInConsumer(ctx context.Context) error {
	consumerObj, err := gr.getConsumerObj(ctx)
	if err != nil {
		return fmt.Errorf("failed to get resource from consumer cluster %q: %w", gr.consumerName, err)
	}

	if controllerutil.AddFinalizer(consumerObj, genericFinalizer) {
		if err := gr.consumerCluster.GetClient().Update(ctx, consumerObj); err != nil {
			return fmt.Errorf("failed to add finalizer in consumer: %w", err)
		}
	}

	anns := consumerObj.GetAnnotations()
	if anns == nil {
		anns = make(map[string]string)
	}

	switch gr.providerName {
	case "":
		delete(anns, providerClusterAnn)
	default:
		anns[providerClusterAnn] = gr.providerName
	}

	switch gr.newProviderName {
	case "":
		delete(anns, newProviderClusterAnn)
	default:
		anns[newProviderClusterAnn] = gr.newProviderName
	}

	consumerObj.SetAnnotations(anns)
	if err := gr.consumerCluster.GetClient().Update(ctx, consumerObj); err != nil {
		return fmt.Errorf("failed to set annotations in consumer: %w", err)
	}

	return nil
}

func (gr *genericReconciler) newProvider(ctx context.Context, consumerObj *unstructured.Unstructured) error {
	var err error
	gr.newProviderName, err = gr.getNewProviderName(consumerObj)
	if err != nil {
		return fmt.Errorf("failed to determine new provider cluster: %w", err)
	}
	if gr.newProviderName == "" {
		return fmt.Errorf("no new provider cluster annotation found, cannot migrate")
	}

	gr.log.Info("Determined new provider cluster", "newProvider", gr.newProviderName)

	gr.newProviderCluster, err = gr.opts.GetProviderCluster(ctx, gr.newProviderName)
	if err != nil {
		return fmt.Errorf("failed to get new provider cluster %q: %w", gr.newProviderName, err)
	}

	consumerObj, err = gr.getConsumerObj(ctx)
	if err != nil {
		return fmt.Errorf("failed to get resource from consumer cluster %q: %w", gr.consumerName, err)
	}
	kubernetes.SetAnnotation(consumerObj, newProviderClusterAnn, gr.newProviderName)
	if err := gr.consumerCluster.GetClient().Update(ctx, consumerObj); err != nil {
		return fmt.Errorf("failed to set new provider cluster annotation in consumer: %w", err)
	}

	if err := gr.syncResource(ctx, gr.newProviderName, gr.newProviderCluster); err != nil {
		return fmt.Errorf("failed to sync resource to new provider cluster %q: %w", gr.newProviderName, err)
	}

	return nil
}

// migrate handles migration of the resource from one provider to
// another.
// The first boolean returns whether the reconciliation can continue.
func (gr *genericReconciler) migrate(ctx context.Context, consumerObj *unstructured.Unstructured) (bool, brokerv1alpha1.MigrationState, error) {
	from := metav1.GroupVersionKind{
		Group:   gr.gvk.Group,
		Version: gr.gvk.Version,
		Kind:    gr.gvk.Kind,
	}
	to := metav1.GroupVersionKind{
		Group:   gr.gvk.Group,
		Version: gr.gvk.Version,
		Kind:    gr.gvk.Kind,
	}
	migrationConfig, found := gr.opts.GetMigrationConfiguration(from, to)
	if !found {
		gr.log.Info("No migration configuration found, continuing", "from", from, "to", to)
		return true, brokerv1alpha1.MigrationStateCutoverCompleted, nil
	}

	migration := &brokerv1alpha1.Migration{}
	err := gr.opts.Coordination.Get(
		ctx,
		types.NamespacedName{
			Name:      consumerObj.GetName(),
			Namespace: consumerObj.GetNamespace(),
		},
		migration,
	)
	if err != nil && !apierrors.IsNotFound(err) {
		gr.log.Error(err, "Failed to get Migration resource")
		return false, brokerv1alpha1.MigrationStateUnknown, fmt.Errorf("failed to get Migration resource: %w", err)
	}
	if err == nil {
		gr.log.Info("Found existing Migration")
		return true, migration.Status.State, nil
	}

	gr.log.Info("No existing migration found, creating new migration")
	migration = &brokerv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      consumerObj.GetName(),      // TODO unique name?
			Namespace: consumerObj.GetNamespace(), // TODO predetermined namespace?
		},
		Spec: brokerv1alpha1.MigrationSpec{
			From: brokerv1alpha1.MigrationRef{
				GVK:         migrationConfig.Spec.From,
				Name:        consumerObj.GetName(),
				Namespace:   consumerObj.GetNamespace(),
				ClusterName: gr.providerName,
			},
			To: brokerv1alpha1.MigrationRef{
				GVK:         migrationConfig.Spec.To,
				Name:        consumerObj.GetName(),
				Namespace:   consumerObj.GetNamespace(),
				ClusterName: gr.newProviderName,
			},
		},
	}

	gr.log.Info("Creating migration config in coordination cluster")
	if err := gr.opts.Coordination.Create(ctx, migration); err != nil {
		return false, brokerv1alpha1.MigrationStateUnknown, fmt.Errorf("failed to create Migration resource in coordination cluster: %w", err)
	}

	// Created migration, wait for next reconciliation to continue.
	// At this point the migration reconciler should take over.
	return false, brokerv1alpha1.MigrationStateUnknown, nil
}

func (gr *genericReconciler) decorateInProvider(ctx context.Context, providerName string, providerCluster cluster.Cluster) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gr.gvk)
	if err := providerCluster.GetClient().Get(ctx, gr.req.NamespacedName, obj); err != nil {
		return fmt.Errorf("failed to get resource from provider cluster %q: %w", providerName, err)
	}

	if controllerutil.AddFinalizer(obj, genericFinalizer) {
		if err := providerCluster.GetClient().Update(ctx, obj); err != nil {
			return fmt.Errorf("failed to add finalizer in provider: %w", err)
		}
	}

	kubernetes.SetAnnotation(obj, consumerClusterAnn, gr.consumerName)
	if err := providerCluster.GetClient().Update(ctx, obj); err != nil {
		return fmt.Errorf("failed to set annotations in provider: %w", err)
	}
	return nil
}

func (gr *genericReconciler) providerAcceptsObj(ctx context.Context) (bool, error) {
	obj, err := gr.getConsumerObj(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get resource from consumer cluster %q: %w", gr.consumerName, err)
	}

	acceptAPIs, err := gr.opts.GetProviderAcceptedAPIs(gr.providerName, gr.gvr)
	if err != nil {
		return false, err
	}
	for _, acceptAPI := range acceptAPIs {
		gr.log.Info("Checking provider AcceptAPI", "provider", gr.providerName, "acceptAPI", acceptAPI.Name)
		applies, reasons := acceptAPI.AppliesTo(gr.gvr, obj)
		if applies {
			gr.log.Info("Provider AcceptAPI accepts obj", "provider", gr.providerName, "acceptAPI", acceptAPI.Name)
			return true, nil
		}
		gr.log.Info("Provider AcceptAPI does not accept obj", "provider", gr.providerName, "acceptAPI", acceptAPI.Name, "reasons", reasons)
	}
	return false, nil
}

func (gr *genericReconciler) syncResource(ctx context.Context, providerName string, providerCluster cluster.Cluster) error {
	gr.log.Info("Syncing resource between consumer and provider cluster")
	// TODO send conditions back to consumer cluster
	// TODO there should be two informers triggering this - one
	// for consumer and one for provider
	if cond, err := sync.CopyResource(
		ctx,
		gr.gvk,
		gr.req.NamespacedName,
		gr.consumerCluster.GetClient(),
		providerCluster.GetClient(),
	); err != nil {
		gr.log.Error(err, "Failed to copy resource to provider cluster", "condition", cond)
		return err
	}

	return gr.decorateInProvider(ctx, providerName, providerCluster)
}

func (gr *genericReconciler) getProviderStatus(ctx context.Context) (brokerv1alpha1.Status, bool, error) {
	providerObj, err := gr.getProviderObj(ctx)
	if err != nil {
		return brokerv1alpha1.StatusUnknown, false, fmt.Errorf("failed to get resource from provider cluster %q: %w", gr.providerName, err)
	}

	statusI, found, err := unstructured.NestedString(providerObj.Object, "status", "status")
	return brokerv1alpha1.Status(statusI), found, err
}

func (gr *genericReconciler) getNewProviderStatus(ctx context.Context) (brokerv1alpha1.Status, bool, error) {
	newProviderObj, err := gr.getNewProviderObj(ctx)
	if err != nil {
		return brokerv1alpha1.StatusUnknown, false, fmt.Errorf("failed to get resource from new provider cluster %q: %w", gr.newProviderName, err)
	}

	statusI, found, err := unstructured.NestedString(newProviderObj.Object, "status", "status")
	return brokerv1alpha1.Status(statusI), found, err
}

func (gr *genericReconciler) syncRelatedResources(ctx context.Context, providerName string, providerCluster cluster.Cluster) error {
	// TODO handle resource drift when a related resource is removed in
	// the provider it needs to be removed in the consumer
	// maybe just a finalizer on the resources in the provider?
	relatedResources, err := sync.CollectRelatedResources(ctx, providerCluster.GetClient(), gr.gvk, gr.req.NamespacedName)
	if err != nil {
		return fmt.Errorf("failed to collect related resources from provider cluster %q: %w", providerName, err)
	}

	if len(relatedResources) == 0 {
		gr.log.Info("No related resources to sync from provider to consumer")
		return nil
	}
	gr.log.Info("Syncing related resources from provider to consumer", "count", len(relatedResources))

	consumerObj, err := gr.getConsumerObj(ctx)
	if err != nil {
		return fmt.Errorf("failed to get resource from consumer cluster %q: %w", gr.consumerName, err)
	}

	var errs error
	for key, relatedResource := range relatedResources {
		if err := gr.syncRelatedResource(ctx, providerCluster, key, relatedResource, consumerObj); err != nil {
			errs = errors.Join(errs, err)
		}
	}

	return errs
}

func (gr *genericReconciler) syncRelatedResource(ctx context.Context, providerCluster cluster.Cluster, key string, relatedResource brokerv1alpha1.RelatedResource, consumerObj *unstructured.Unstructured) error {
	log := gr.log.WithValues("relatedResourceKey", key)
	log.Info("Syncing related resource", "relatedResource", relatedResource)
	providerRRObj := &unstructured.Unstructured{}
	providerRRObj.SetGroupVersionKind(relatedResource.SchemaGVK())
	if err := providerCluster.GetClient().Get(
		ctx,
		client.ObjectKey{
			Namespace: relatedResource.Namespace,
			Name:      relatedResource.Name,
		},
		providerRRObj,
	); err != nil {
		log.Error(err, "Failed to get related resource from provider cluster", "relatedResource", relatedResource)
		return err
	}

	// TODO conditions
	_, err := sync.CopyResource(
		ctx,
		relatedResource.SchemaGVK(),
		types.NamespacedName{
			Namespace: relatedResource.Namespace, // TODO namespace from consumer?
			Name:      relatedResource.Name,
		},
		providerCluster.GetClient(),
		gr.consumerCluster.GetClient(),
	)
	if err != nil {
		log.Error(err, "Failed to copy related resource to consumer cluster")
		return err
	}

	log.Info("Getting synced resource from consumer cluster")
	consumerRRObj := &unstructured.Unstructured{}
	consumerRRObj.SetGroupVersionKind(relatedResource.SchemaGVK())
	if err := gr.consumerCluster.GetClient().Get(
		ctx,
		client.ObjectKey{
			Namespace: relatedResource.Namespace,
			Name:      relatedResource.Name,
		},
		consumerRRObj,
	); err != nil {
		log.Error(err, "Failed to get synced related resource from consumer cluster")
		return err
	}

	log.Info("Setting owner reference on related resource in consumer cluster")
	if err := controllerutil.SetOwnerReference(consumerObj, consumerRRObj, gr.consumerCluster.GetScheme()); err != nil {
		log.Error(err, "Failed to set owner reference on related resource in consumer cluster")
		return err
	}

	if err := gr.consumerCluster.GetClient().Update(ctx, consumerRRObj); err != nil {
		log.Error(err, "Failed to set owner reference on related resource in consumer cluster")
		return err
	}
	return nil
}
