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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mctrl "sigs.k8s.io/multicluster-runtime"
	mchandler "sigs.k8s.io/multicluster-runtime/pkg/handler"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
	"github.com/platform-mesh/resource-broker/pkg/kubernetes"
	"github.com/platform-mesh/resource-broker/pkg/sync"
)

const (
	genericFinalizer      = "broker.platform-mesh.io/generic-finalizer"
	consumerClusterAnn    = "broker.platform-mesh.io/consumer-cluster"
	consumerNameAnn       = "broker.platform-mesh.io/consumer-name"
	providerClusterAnn    = "broker.platform-mesh.io/provider-cluster"
	newProviderClusterAnn = "broker.platform-mesh.io/new-provider-cluster"
)

// Options are the options for the generic reconciler.
type Options struct {
	ControllerNamePrefix      string
	CoordinationClient        client.Client
	GetProviderCluster        func(context.Context, multicluster.ClusterName) (cluster.Cluster, error)
	GetConsumerCluster        func(context.Context, multicluster.ClusterName) (cluster.Cluster, error)
	GetProviders              func(metav1.GroupVersionResource) map[string]map[string]brokerv1alpha1.AcceptAPI
	GetProviderAcceptedAPIs   func(string, metav1.GroupVersionResource) ([]brokerv1alpha1.AcceptAPI, error)
	GetMigrationConfiguration func(metav1.GroupVersionKind, metav1.GroupVersionKind) (brokerv1alpha1.MigrationConfiguration, bool)
}

// SetupController creates a controller for the resource specified by GVK.
func SetupController(mgr mctrl.Manager, gvk schema.GroupVersionKind, opts Options) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)

	// providerEventHandler maps events from provider resources back to the
	// consumer resource that owns them. This enables status updates on the
	// provider side to trigger reconciliation and sync status back to consumer.
	providerEventHandler := mchandler.TypedEnqueueRequestsFromMapFunc[client.Object, mctrl.Request](
		func(_ context.Context, obj client.Object) []mctrl.Request {
			annotations := obj.GetAnnotations()
			if annotations == nil {
				return nil
			}
			consumerCluster, ok := annotations[consumerClusterAnn]
			if !ok || consumerCluster == "" {
				return nil
			}
			consumerName, ok := annotations[consumerNameAnn]
			if !ok || consumerName == "" {
				return nil
			}
			return []mctrl.Request{{
				ClusterName: multicluster.ClusterName(consumerCluster),
				Request: reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: obj.GetNamespace(),
						Name:      consumerName,
					},
				},
			}}
		},
	)

	return mctrl.NewControllerManagedBy(mgr).
		Named(opts.ControllerNamePrefix+"-generic-"+gvk.String()).
		For(obj).
		Watches(obj, providerEventHandler).
		Complete(mcreconcile.Func(func(ctx context.Context, req mctrl.Request) (mctrl.Result, error) {
			task := &objectReconcileTask{
				opts: opts,
				gvk:  gvk,
				req:  req,
			}

			return task.Run(ctx)
		}))
}

type objectReconcileTask struct {
	opts Options
	log  logr.Logger
	gvr  metav1.GroupVersionResource
	gvk  schema.GroupVersionKind
	req  mctrl.Request

	consumerName       string
	consumerCluster    cluster.Cluster
	providerName       string
	providerCluster    cluster.Cluster
	newProviderName    string
	newProviderCluster cluster.Cluster

	// consumerObjName holds the original NamespacedName of the resource
	// in the consumer cluster. It is set when a reconcile is triggered from
	// the provider side, where gr.req.NamespacedName contains the
	// provider-side name (prefixed with hashed consumer cluster name).
	consumerObjName *types.NamespacedName
}

// SanitizeClusterName hashes a cluster name into a 12-char hex string
// safe for use in Kubernetes resource names.
func SanitizeClusterName(name string) string {
	h := sha256.Sum256([]byte(name))
	return hex.EncodeToString(h[:6])
}

// providerNamespacedName returns the NamespacedName used to store the
// resource in the provider cluster. The name is prefixed with a hash of
// the consumer cluster name, keeping the namespace unchanged. This avoids
// conflicts when multiple consumers create resources with the same name
// and namespace.
func (t *objectReconcileTask) providerNamespacedName() types.NamespacedName {
	srcName := t.req.NamespacedName
	if t.consumerObjName != nil {
		srcName = *t.consumerObjName
	}
	return types.NamespacedName{
		Namespace: srcName.Namespace,
		Name:      SanitizeClusterName(t.consumerName) + "-" + srcName.Name,
	}
}

// consumerNamespacedName returns the original (unprefixed) NamespacedName
// of the resource in the consumer cluster.
func (t *objectReconcileTask) consumerNamespacedName() types.NamespacedName {
	if t.consumerObjName != nil {
		return *t.consumerObjName
	}
	return t.req.NamespacedName
}

func (t *objectReconcileTask) Run(ctx context.Context) (mctrl.Result, error) {
	t.log = logr.FromContextOrDiscard(ctx).WithValues("gvk", t.gvk, "name", t.req.NamespacedName, "cluster", t.req.ClusterName)
	t.log.Info("Reconciling generic resource")

	cont, err := t.determineClusters(ctx)
	if err != nil {
		t.log.Error(err, "Failed to determine clusters")
		return mctrl.Result{}, err
	}
	if !cont {
		// Not continuing
		return mctrl.Result{}, nil
	}

	if t.consumerCluster == nil {
		t.log.Info("Consumer cluster is not set, skipping")
		return mctrl.Result{}, t.deleteObjs(ctx)
	}

	t.log = t.log.WithValues("consumer", t.consumerName, "provider", t.providerName)

	consumerObj, err := t.getConsumerObj(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return mctrl.Result{}, t.deleteObjs(ctx)
		}
		return mctrl.Result{}, fmt.Errorf("failed to get resource from consumer cluster %q: %w", t.consumerName, err)
	}

	if !consumerObj.GetDeletionTimestamp().IsZero() {
		t.log.Info("Resource in consumer cluster is being deleted, finalizing")
		return mctrl.Result{}, t.deleteObjs(ctx)
	}

	if err := t.decorateInConsumer(ctx); err != nil {
		return mctrl.Result{}, err
	}

	providerAccepts := true
	// Only check provider acceptance if there isn't already a migration
	// going on.
	if t.newProviderCluster == nil {
		var err error
		providerAccepts, err = t.providerAcceptsObj(ctx)
		if err != nil {
			return mctrl.Result{}, err
		}
	}

	if t.newProviderCluster != nil || !providerAccepts {
		t.log.Info("Provider no longer accepts resource")
		if err := t.newProvider(ctx, consumerObj); err != nil {
			return mctrl.Result{}, err
		}

		status, found, err := t.getNewProviderStatus(ctx)
		if err != nil {
			return mctrl.Result{}, err
		}
		if found && !status.Continue() {
			return mctrl.Result{}, nil
		}

		cont, state, err := t.migrate(ctx, consumerObj)
		if err != nil {
			t.log.Error(err, "Failed to check migration status")
			return mctrl.Result{}, err
		}
		if !cont {
			t.log.Info("Migration not yet ready to continue, waiting")
			return mctrl.Result{RequeueAfter: time.Second}, nil
		}

		// Copy related resources from new provider when the cutover
		// to the new provider can start.
		switch state {
		case brokerv1alpha1.MigrationStateInitialCompleted, brokerv1alpha1.MigrationStateCutoverInProgress, brokerv1alpha1.MigrationStateCutoverCompleted:
			t.log.Info("Syncing related resources from new provider")
			if err := t.syncRelatedResources(ctx, t.newProviderName, t.newProviderCluster); err != nil {
				return mctrl.Result{}, err
			}
		}

		if state != brokerv1alpha1.MigrationStateCutoverCompleted {
			t.log.Info("Migration not yet completed, waiting")
			return mctrl.Result{RequeueAfter: time.Second}, nil
		}

		t.log.Info("Deleting from old provider")
		if err := t.deleteObj(ctx, t.providerCluster, t.providerNamespacedName()); err != nil {
			return mctrl.Result{}, fmt.Errorf("failed to delete resource from old provider cluster %q: %w", t.providerName, err)
		}

		t.providerName = t.newProviderName
		t.providerCluster = t.newProviderCluster
		t.newProviderName = ""
		t.newProviderCluster = nil

		return mctrl.Result{Requeue: true}, t.decorateInConsumer(ctx)
	}

	if err := t.syncResource(ctx, t.providerName, t.providerCluster); err != nil {
		return mctrl.Result{}, err
	}

	status, found, err := t.getProviderStatus(ctx)
	if err != nil {
		t.log.Error(err, "Failed to get provider status")
		return mctrl.Result{}, err
	}
	if found && !status.Continue() {
		t.log.Info("Provider status indicates to not continue")
		return mctrl.Result{}, nil
	}

	if err := t.syncRelatedResources(ctx, t.providerName, t.providerCluster); err != nil {
		return mctrl.Result{}, err
	}

	return mctrl.Result{}, nil
}

func (t *objectReconcileTask) getPossibleProvider(obj *unstructured.Unstructured) (string, error) {
	possibleProviders := t.opts.GetProviders(t.gvr)
	if len(possibleProviders) == 0 {
		return "", fmt.Errorf("no clusters accept GVR %v", t.gvr)
	}

	for possibleProvider, acceptedAPIs := range possibleProviders {
		for _, acceptAPI := range acceptedAPIs {
			applies, reasons := acceptAPI.AppliesTo(t.gvr, obj)
			if applies {
				return possibleProvider, nil
			}
			t.log.Info("Provider does not accept resource due to filter mismatch", "provider", possibleProvider, "reasons", reasons)
		}
	}

	return "", fmt.Errorf("no accepting cluster found for GVR %v", t.gvr)
}

func (t *objectReconcileTask) getProviderName(obj *unstructured.Unstructured) (string, error) {
	providerName, ok := obj.GetAnnotations()[providerClusterAnn]
	if ok && providerName != "" {
		t.log.Info("Found provider cluster annotation", "provider", providerName)
		return providerName, nil
	}

	t.log.Info("Found no provider in annotations, looking for possible providers")
	return t.getPossibleProvider(obj)
}

func (t *objectReconcileTask) getNewProviderName(obj *unstructured.Unstructured) (string, error) {
	providerName, ok := obj.GetAnnotations()[newProviderClusterAnn]
	if ok && providerName != "" {
		t.log.Info("Found new provider cluster annotation", "newProvider", providerName)
		return providerName, nil
	}

	return t.getPossibleProvider(obj)
}

func (t *objectReconcileTask) determineClusters(ctx context.Context) (bool, error) {
	// This made more sense before the logic was moved into its own
	// package. Should refactor this when time permits.

	if _, err := t.opts.GetConsumerCluster(ctx, t.req.ClusterName); err == nil {
		t.log.Info("Request comes from consumer cluster")
		if err := t.setConsumerCluster(ctx, string(t.req.ClusterName)); err != nil {
			return false, fmt.Errorf("failed to set consumer cluster: %w", err)
		}
	}

	if _, err := t.opts.GetProviderCluster(ctx, t.req.ClusterName); err == nil {
		t.log.Info("Request comes from provider cluster")
		if err := t.setProviderCluster(ctx, string(t.req.ClusterName)); err != nil {
			return false, fmt.Errorf("failed to set provider cluster: %w", err)
		}
	}

	if t.consumerName == "" && t.providerName == "" {
		t.log.Info("Request does not come from known consumer or provider cluster, skipping")
		return false, nil
	}

	if t.consumerName == "" {
		if err := t.setConsumerClusterFromProvider(ctx); err != nil {
			return false, err
		}
		// If consumer cluster is still nil the request cannot be
		// served. Cause might be that the same resource exists in
		// a provider cluster but doesn't originate from the broker.
		if t.consumerCluster == nil {
			return false, nil
		}
	}

	// GVR must be set here because it is needed to find possible
	// providers based on accepted APIs
	var err error
	t.gvr, err = t.getGVR()
	if err != nil {
		t.log.Error(err, "Failed to determine GVR for resource")
		return false, err
	}

	if t.providerName == "" {
		if err := t.setProviderClusterFromConsumer(ctx); err != nil {
			return false, err
		}
	}

	// Do a sanity check so an event from the new provider cluster does
	// not start overwriting things from the new provider cluster before
	// the migration is done.
	consumerObj, err := t.getConsumerObj(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Consumer object not found, nothing to do
			return false, nil
		}
		return false, fmt.Errorf("failed to get resource from consumer cluster %q: %w", t.consumerName, err)
	}

	var ok bool
	t.newProviderName, ok = consumerObj.GetAnnotations()[newProviderClusterAnn]
	if !ok {
		// No new provider annotation, continue
		return true, nil
	}
	t.log.Info("Found new provider annotation", "newProvider", t.newProviderName)

	if string(t.req.ClusterName) == t.newProviderName {
		t.log.Info("Event comes from new provider cluster")
		t.newProviderCluster = t.providerCluster
		if err := t.setProviderCluster(ctx, consumerObj.GetAnnotations()[providerClusterAnn]); err != nil {
			return false, err
		}
		return true, nil
	}

	t.newProviderCluster, err = t.opts.GetProviderCluster(ctx, multicluster.ClusterName(t.newProviderName))
	if err != nil {
		return false, fmt.Errorf("failed to get new provider cluster %q: %w", t.newProviderName, err)
	}

	return true, nil
}

func (t *objectReconcileTask) setConsumerCluster(ctx context.Context, name string) error {
	t.log.Info("Setting consumer cluster", "consumer", name)
	cl, err := t.opts.GetConsumerCluster(ctx, multicluster.ClusterName(name))
	if err != nil {
		return fmt.Errorf("failed to get consumer cluster %q: %w", name, err)
	}
	t.consumerName = name
	t.consumerCluster = cl
	return nil
}

func (t *objectReconcileTask) setConsumerClusterFromProvider(ctx context.Context) error {
	t.log.Info("Determining consumer cluster from provider annotation")

	// When the request comes from the provider side, t.req.NamespacedName
	// contains the prefixed name. We can fetch the object directly since
	// the provider cluster uses the prefixed name.
	providerObj := &unstructured.Unstructured{}
	providerObj.SetGroupVersionKind(t.gvk)
	if err := t.providerCluster.GetClient().Get(ctx, t.req.NamespacedName, providerObj); err != nil {
		if apierrors.IsNotFound(err) {
			// If the provider object is not found, the consumer cluster
			// cannot be set based on its annotation.
			return nil
		}
		return fmt.Errorf("failed to get resource from provider cluster %q: %w", t.providerName, err)
	}

	anns := providerObj.GetAnnotations()

	consumerCluster, ok := anns[consumerClusterAnn]
	if !ok || consumerCluster == "" {
		t.log.Info("Resource in provider cluster missing consumer cluster annotation, skipping")
		return nil
	}

	// Read the original consumer name from the annotation so that
	// consumer-side lookups use the unprefixed name.
	if origName, ok := anns[consumerNameAnn]; ok && origName != "" {
		t.consumerObjName = &types.NamespacedName{
			Namespace: t.req.Namespace,
			Name:      origName,
		}
	}

	t.log.Info("Found consumer cluster annotation in provider", "consumer", consumerCluster)
	return t.setConsumerCluster(ctx, consumerCluster)
}

func (t *objectReconcileTask) setProviderCluster(ctx context.Context, name string) error {
	t.log.Info("Setting provider cluster", "provider", name)
	cl, err := t.opts.GetProviderCluster(ctx, multicluster.ClusterName(name))
	if err != nil {
		return fmt.Errorf("failed to get provider cluster %q: %w", name, err)
	}
	t.providerName = name
	t.providerCluster = cl
	return nil
}

func (t *objectReconcileTask) setProviderClusterFromConsumer(ctx context.Context) error {
	t.log.Info("Determining provider cluster from consumer annotation")
	consumerObj, err := t.getConsumerObj(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the consumer object is not found, the provider cluster
			// cannot be set based on its annotation.
			return nil
		}
		return fmt.Errorf("failed to get resource from consumer cluster %q: %w", t.consumerName, err)
	}

	possibleProviderName, err := t.getProviderName(consumerObj)
	if err != nil {
		return fmt.Errorf("failed to determine provider cluster: %w", err)
	}
	if possibleProviderName == "" {
		return fmt.Errorf("no present or possible provider cluster found %q: %w", t.consumerName, err)
	}

	t.log.Info("Determined provider cluster", "provider", possibleProviderName)
	return t.setProviderCluster(ctx, possibleProviderName)
}

func (t *objectReconcileTask) getGVR() (metav1.GroupVersionResource, error) {
	if t.consumerCluster == nil {
		return metav1.GroupVersionResource{}, fmt.Errorf("consumer cluster is not set")
	}
	mapper := t.consumerCluster.GetRESTMapper()
	mapping, err := mapper.RESTMapping(t.gvk.GroupKind(), t.gvk.Version)
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

func (t *objectReconcileTask) getConsumerObj(ctx context.Context) (*unstructured.Unstructured, error) {
	consumerObj := &unstructured.Unstructured{}
	consumerObj.SetGroupVersionKind(t.gvk)
	if err := t.consumerCluster.GetClient().Get(ctx, t.consumerNamespacedName(), consumerObj); err != nil {
		return nil, err
	}
	return consumerObj, nil
}

func (t *objectReconcileTask) getProviderObj(ctx context.Context) (*unstructured.Unstructured, error) {
	providerObj := &unstructured.Unstructured{}
	providerObj.SetGroupVersionKind(t.gvk)
	if err := t.providerCluster.GetClient().Get(ctx, t.providerNamespacedName(), providerObj); err != nil {
		return nil, err
	}
	return providerObj, nil
}

func (t *objectReconcileTask) getNewProviderObj(ctx context.Context) (*unstructured.Unstructured, error) {
	newProviderObj := &unstructured.Unstructured{}
	newProviderObj.SetGroupVersionKind(t.gvk)
	if err := t.newProviderCluster.GetClient().Get(ctx, t.providerNamespacedName(), newProviderObj); err != nil {
		return nil, err
	}
	return newProviderObj, nil
}

func (t *objectReconcileTask) deleteObjs(ctx context.Context) error {
	if t.providerCluster != nil {
		if err := t.deleteObj(ctx, t.providerCluster, t.providerNamespacedName()); err != nil {
			return fmt.Errorf("failed to delete resource from provider cluster %q: %w", t.providerName, err)
		}
	}

	if t.consumerCluster != nil {
		if err := t.deleteObj(ctx, t.consumerCluster, t.consumerNamespacedName()); err != nil {
			return fmt.Errorf("failed to delete resource from consumer cluster %q: %w", t.consumerName, err)
		}
	}

	return nil
}

func (t *objectReconcileTask) deleteObj(ctx context.Context, cl cluster.Cluster, name types.NamespacedName) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(t.gvk)
	if err := cl.GetClient().Get(ctx, name, obj); err != nil {
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

func (t *objectReconcileTask) decorateInConsumer(ctx context.Context) error {
	consumerObj, err := t.getConsumerObj(ctx)
	if err != nil {
		return fmt.Errorf("failed to get resource from consumer cluster %q: %w", t.consumerName, err)
	}

	if controllerutil.AddFinalizer(consumerObj, genericFinalizer) {
		if err := t.consumerCluster.GetClient().Update(ctx, consumerObj); err != nil {
			return fmt.Errorf("failed to add finalizer in consumer: %w", err)
		}
	}

	anns := consumerObj.GetAnnotations()
	if anns == nil {
		anns = make(map[string]string)
	}

	changed := false
	switch t.providerName {
	case "":
		if _, ok := anns[providerClusterAnn]; ok {
			delete(anns, providerClusterAnn)
			changed = true
		}
	default:
		if anns[providerClusterAnn] != t.providerName {
			anns[providerClusterAnn] = t.providerName
			changed = true
		}
	}

	switch t.newProviderName {
	case "":
		if _, ok := anns[newProviderClusterAnn]; ok {
			delete(anns, newProviderClusterAnn)
			changed = true
		}
	default:
		if anns[newProviderClusterAnn] != t.newProviderName {
			anns[newProviderClusterAnn] = t.newProviderName
			changed = true
		}
	}

	if changed {
		consumerObj.SetAnnotations(anns)
		if err := t.consumerCluster.GetClient().Update(ctx, consumerObj); err != nil {
			return fmt.Errorf("failed to set annotations in consumer: %w", err)
		}
	}

	return nil
}

func (t *objectReconcileTask) newProvider(ctx context.Context, consumerObj *unstructured.Unstructured) error {
	var err error
	t.newProviderName, err = t.getNewProviderName(consumerObj)
	if err != nil {
		return fmt.Errorf("failed to determine new provider cluster: %w", err)
	}
	if t.newProviderName == "" {
		return fmt.Errorf("no new provider cluster annotation found, cannot migrate")
	}

	t.log.Info("Determined new provider cluster", "newProvider", t.newProviderName)

	t.newProviderCluster, err = t.opts.GetProviderCluster(ctx, multicluster.ClusterName(t.newProviderName))
	if err != nil {
		return fmt.Errorf("failed to get new provider cluster %q: %w", t.newProviderName, err)
	}

	consumerObj, err = t.getConsumerObj(ctx)
	if err != nil {
		return fmt.Errorf("failed to get resource from consumer cluster %q: %w", t.consumerName, err)
	}
	kubernetes.SetAnnotation(consumerObj, newProviderClusterAnn, t.newProviderName)
	if err := t.consumerCluster.GetClient().Update(ctx, consumerObj); err != nil {
		return fmt.Errorf("failed to set new provider cluster annotation in consumer: %w", err)
	}

	if err := t.syncResource(ctx, t.newProviderName, t.newProviderCluster); err != nil {
		return fmt.Errorf("failed to sync resource to new provider cluster %q: %w", t.newProviderName, err)
	}

	return nil
}

// migrate handles migration of the resource from one provider to another.
// The first boolean returns whether the reconciliation can continue.
func (t *objectReconcileTask) migrate(ctx context.Context, consumerObj *unstructured.Unstructured) (bool, brokerv1alpha1.MigrationState, error) {
	from := metav1.GroupVersionKind{
		Group:   t.gvk.Group,
		Version: t.gvk.Version,
		Kind:    t.gvk.Kind,
	}
	to := metav1.GroupVersionKind{
		Group:   t.gvk.Group,
		Version: t.gvk.Version,
		Kind:    t.gvk.Kind,
	}
	migrationConfig, found := t.opts.GetMigrationConfiguration(from, to)
	if !found {
		t.log.Info("No migration configuration found, continuing", "from", from, "to", to)
		return true, brokerv1alpha1.MigrationStateCutoverCompleted, nil
	}

	migration := &brokerv1alpha1.Migration{}
	err := t.opts.CoordinationClient.Get(
		ctx,
		types.NamespacedName{
			Name:      consumerObj.GetName(),
			Namespace: consumerObj.GetNamespace(),
		},
		migration,
	)
	if err != nil && !apierrors.IsNotFound(err) {
		t.log.Error(err, "Failed to get Migration resource")
		return false, brokerv1alpha1.MigrationStateUnknown, fmt.Errorf("failed to get Migration resource: %w", err)
	}
	if err == nil {
		t.log.Info("Found existing Migration")
		return true, migration.Status.State, nil
	}

	t.log.Info("No existing migration found, creating new migration")
	providerNN := t.providerNamespacedName()
	migration = &brokerv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      consumerObj.GetName(),      // TODO unique name?
			Namespace: consumerObj.GetNamespace(), // TODO predetermined namespace?
		},
		Spec: brokerv1alpha1.MigrationSpec{
			From: brokerv1alpha1.MigrationRef{
				GVK:         migrationConfig.Spec.From,
				Name:        providerNN.Name,
				Namespace:   providerNN.Namespace,
				ClusterName: t.providerName,
			},
			To: brokerv1alpha1.MigrationRef{
				GVK:         migrationConfig.Spec.To,
				Name:        providerNN.Name,
				Namespace:   providerNN.Namespace,
				ClusterName: t.newProviderName,
			},
		},
	}

	t.log.Info("Creating migration config in coordination cluster")
	if err := t.opts.CoordinationClient.Create(ctx, migration); err != nil {
		return false, brokerv1alpha1.MigrationStateUnknown, fmt.Errorf("failed to create Migration resource in coordination cluster: %w", err)
	}

	// Created migration, wait for next reconciliation to continue.
	// At this point the migration reconciler should take over.
	return false, brokerv1alpha1.MigrationStateUnknown, nil
}

func (t *objectReconcileTask) decorateInProvider(ctx context.Context, providerName string, providerCluster cluster.Cluster) error {
	consumerNN := t.consumerNamespacedName()

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(t.gvk)
		if err := providerCluster.GetClient().Get(ctx, t.providerNamespacedName(), obj); err != nil {
			return fmt.Errorf("failed to get resource from provider cluster %q: %w", providerName, err)
		}

		var needsUpdate bool
		if controllerutil.AddFinalizer(obj, genericFinalizer) {
			needsUpdate = true
		}

		anns := obj.GetAnnotations()
		if anns[consumerClusterAnn] != t.consumerName || anns[consumerNameAnn] != consumerNN.Name {
			kubernetes.SetAnnotation(obj, consumerClusterAnn, t.consumerName)
			kubernetes.SetAnnotation(obj, consumerNameAnn, consumerNN.Name)
			needsUpdate = true
		}

		if needsUpdate {
			if err := providerCluster.GetClient().Update(ctx, obj); err != nil {
				return err
			}
		}
		return nil
	})
}

func (t *objectReconcileTask) providerAcceptsObj(ctx context.Context) (bool, error) {
	obj, err := t.getConsumerObj(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get resource from consumer cluster %q: %w", t.consumerName, err)
	}

	acceptAPIs, err := t.opts.GetProviderAcceptedAPIs(t.providerName, t.gvr)
	if err != nil {
		return false, err
	}
	for _, acceptAPI := range acceptAPIs {
		t.log.Info("Checking provider AcceptAPI", "provider", t.providerName, "acceptAPI", acceptAPI.Name)
		applies, reasons := acceptAPI.AppliesTo(t.gvr, obj)
		if applies {
			t.log.Info("Provider AcceptAPI accepts obj", "provider", t.providerName, "acceptAPI", acceptAPI.Name)
			return true, nil
		}
		t.log.Info("Provider AcceptAPI does not accept obj", "provider", t.providerName, "acceptAPI", acceptAPI.Name, "reasons", reasons)
	}
	return false, nil
}

func (t *objectReconcileTask) syncResource(ctx context.Context, providerName string, providerCluster cluster.Cluster) error {
	// TODO there should be two informers triggering this - one
	// for consumer and one for provider

	// Create a status transformer that strips the cluster prefix from
	// relatedResources names when syncing status from provider to consumer.
	prefix := SanitizeClusterName(t.consumerName) + "-"
	statusTransformer := func(status any) any {
		return t.transformStatusRelatedResources(status, prefix)
	}

	if cond, err := sync.CopyResource(
		ctx,
		t.gvk,
		t.consumerNamespacedName(),
		t.providerNamespacedName(),
		t.consumerCluster.GetClient(),
		providerCluster.GetClient(),
		sync.CopyResourceOptions{StatusTransformer: statusTransformer},
	); err != nil {
		t.log.Error(err, "Failed to copy resource to provider cluster", "condition", cond)
		return err
	}

	return t.decorateInProvider(ctx, providerName, providerCluster)
}

// transformStatusRelatedResources transforms the status by stripping the
// cluster prefix from relatedResources names. This is needed because on
// the provider side, resource names are prefixed with the hashed consumer
// cluster name, but on the consumer side they should use the original names.
func (t *objectReconcileTask) transformStatusRelatedResources(status any, prefix string) any {
	statusMap, ok := status.(map[string]any)
	if !ok {
		return status
	}

	relatedResources, ok := statusMap["relatedResources"].(map[string]any)
	if !ok {
		return status
	}

	// Deep copy to avoid modifying the original
	newStatus := make(map[string]any)
	for k, v := range statusMap {
		if k == "relatedResources" {
			continue
		}
		newStatus[k] = v
	}

	newRelatedResources := make(map[string]any)
	for key, rr := range relatedResources {
		rrMap, ok := rr.(map[string]any)
		if !ok {
			newRelatedResources[key] = rr
			continue
		}

		newRR := make(map[string]any)
		for k, v := range rrMap {
			if k == "name" {
				if name, ok := v.(string); ok {
					if trimmed, found := strings.CutPrefix(name, prefix); found {
						newRR[k] = trimmed
						continue
					}
				}
			}
			newRR[k] = v
		}
		newRelatedResources[key] = newRR
	}
	newStatus["relatedResources"] = newRelatedResources

	return newStatus
}

func (t *objectReconcileTask) getProviderStatus(ctx context.Context) (brokerv1alpha1.Status, bool, error) {
	providerObj, err := t.getProviderObj(ctx)
	if err != nil {
		return brokerv1alpha1.StatusUnknown, false, fmt.Errorf("failed to get resource from provider cluster %q: %w", t.providerName, err)
	}

	statusI, found, err := unstructured.NestedString(providerObj.Object, "status", "status")
	return brokerv1alpha1.Status(statusI), found, err
}

func (t *objectReconcileTask) getNewProviderStatus(ctx context.Context) (brokerv1alpha1.Status, bool, error) {
	newProviderObj, err := t.getNewProviderObj(ctx)
	if err != nil {
		return brokerv1alpha1.StatusUnknown, false, fmt.Errorf("failed to get resource from new provider cluster %q: %w", t.newProviderName, err)
	}

	statusI, found, err := unstructured.NestedString(newProviderObj.Object, "status", "status")
	return brokerv1alpha1.Status(statusI), found, err
}

func (t *objectReconcileTask) syncRelatedResources(ctx context.Context, providerName string, providerCluster cluster.Cluster) error {
	// TODO handle resource drift when a related resource is removed in
	// the provider it needs to be removed in the consumer
	// maybe just a finalizer on the resources in the provider?
	relatedResources, err := sync.CollectRelatedResources(ctx, providerCluster.GetClient(), t.gvk, t.providerNamespacedName())
	if err != nil {
		return fmt.Errorf("failed to collect related resources from provider cluster %q: %w", providerName, err)
	}

	if len(relatedResources) == 0 {
		t.log.Info("No related resources to sync from provider to consumer")
		return nil
	}
	t.log.Info("Syncing related resources from provider to consumer", "count", len(relatedResources))

	consumerObj, err := t.getConsumerObj(ctx)
	if err != nil {
		return fmt.Errorf("failed to get resource from consumer cluster %q: %w", t.consumerName, err)
	}

	var errs error
	for key, relatedResource := range relatedResources {
		if err := t.syncRelatedResource(ctx, providerCluster, key, relatedResource, consumerObj); err != nil {
			errs = errors.Join(errs, err)
		}
	}

	return errs
}

func (t *objectReconcileTask) syncRelatedResource(ctx context.Context, providerCluster cluster.Cluster, key string, relatedResource brokerv1alpha1.RelatedResource, consumerObj *unstructured.Unstructured) error {
	log := t.log.WithValues("relatedResourceKey", key)
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

	// Related resources on the provider may inherit the hashed prefix from
	// the parent resource name. Strip the prefix so the consumer gets the
	// original unprefixed name.
	providerRRName := types.NamespacedName{
		Namespace: relatedResource.Namespace,
		Name:      relatedResource.Name,
	}
	consumerRRName := relatedResource.Name
	prefix := SanitizeClusterName(t.consumerName) + "-"
	if trimmed, found := strings.CutPrefix(relatedResource.Name, prefix); found {
		consumerRRName = trimmed
	}
	consumerRRNN := types.NamespacedName{
		Namespace: t.consumerNamespacedName().Namespace,
		Name:      consumerRRName,
	}

	// TODO conditions
	_, err := sync.CopyResource(
		ctx,
		relatedResource.SchemaGVK(),
		providerRRName,
		consumerRRNN,
		providerCluster.GetClient(),
		t.consumerCluster.GetClient(),
	)
	if err != nil {
		log.Error(err, "Failed to copy related resource to consumer cluster")
		return err
	}

	log.Info("Getting synced resource from consumer cluster")
	consumerRRObj := &unstructured.Unstructured{}
	consumerRRObj.SetGroupVersionKind(relatedResource.SchemaGVK())
	if err := t.consumerCluster.GetClient().Get(
		ctx,
		consumerRRNN,
		consumerRRObj,
	); err != nil {
		log.Error(err, "Failed to get synced related resource from consumer cluster")
		return err
	}

	log.Info("Setting owner reference on related resource in consumer cluster")
	if err := controllerutil.SetOwnerReference(consumerObj, consumerRRObj, t.consumerCluster.GetScheme()); err != nil {
		log.Error(err, "Failed to set owner reference on related resource in consumer cluster")
		return err
	}

	if err := t.consumerCluster.GetClient().Update(ctx, consumerRRObj); err != nil {
		log.Error(err, "Failed to set owner reference on related resource in consumer cluster")
		return err
	}
	return nil
}
