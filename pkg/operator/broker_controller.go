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

// Package operator contains the reconcilers for resource-broker-operator.
package operator

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	mctrl "sigs.k8s.io/multicluster-runtime"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	operatorv1alpha1 "github.com/platform-mesh/resource-broker/api/operator/v1alpha1"
)

const (
	// ConditionTypeAvailable indicates if the backing Deployment is available.
	ConditionTypeAvailable = "Available"
	// ConditionTypeProgressing indicates if the backing Deployment is currently updating.
	ConditionTypeProgressing = "Progressing"
	// ConditionTypeDegraded indicates if the backing Deployment has unavailable replicas.
	ConditionTypeDegraded = "Degraded"
)

// SetupBrokerController creates a controller for the Broker resource.
func SetupBrokerController(mgr mctrl.Manager, opts BrokerOptions) error {
	return mctrl.NewControllerManagedBy(mgr).
		Named("broker-reconciler").
		For(&operatorv1alpha1.Broker{}).
		Owns(&appsv1.Deployment{}).
		Complete(opts)
}

// BrokerOptions contains data and functions the controller requires to operate.
type BrokerOptions struct {
	Scheme     *runtime.Scheme
	GetCluster func(context.Context, multicluster.ClusterName) (cluster.Cluster, error)
}

// Reconcile reconciles a request for the Broker resource.
func (opts BrokerOptions) Reconcile(ctx context.Context, req mctrl.Request) (mctrl.Result, error) {
	br := &brokerReconciler{
		opts: opts,
		log: ctrllog.FromContext(ctx).WithValues(
			"clusterName", req.ClusterName,
			"name", req.Name,
			"namespace", req.Namespace,
		),
		req: req,
	}
	return br.reconcile(ctx)
}

type brokerReconciler struct {
	opts BrokerOptions
	log  logr.Logger
	req  mctrl.Request

	client client.Client
}

func (r *brokerReconciler) reconcile(ctx context.Context) (mctrl.Result, error) {
	r.log.Info("reconciling")

	cl, err := r.opts.GetCluster(ctx, r.req.ClusterName)
	if err != nil {
		return mctrl.Result{}, err
	}

	r.client = cl.GetClient()

	broker := &operatorv1alpha1.Broker{}
	if err := r.client.Get(ctx, r.req.NamespacedName, broker); err != nil {
		if apierrors.IsNotFound(err) {
			return mctrl.Result{}, nil
		}
		return mctrl.Result{}, err
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      broker.Name,
			Namespace: broker.Namespace,
		},
	}

	result, err := controllerutil.CreateOrPatch(ctx, r.client, deployment, func() error {
		r.log.Info("updating deployment")
		if err := updateDeployment(r.opts.Scheme, broker, deployment); err != nil {
			return fmt.Errorf("error updating deployment: %w", err)
		}
		updateBrokerStatus(broker, deployment)
		return nil
	})
	r.log.Info("deployment reconciled", "result", result)
	if err != nil {
		return mctrl.Result{}, fmt.Errorf("error creating/updating deployment: %w", err)
	}

	result, err = controllerutil.CreateOrPatch(ctx, r.client, broker, func() error {
		r.log.Info("updating broker status")
		updateBrokerStatus(broker, deployment)
		return nil
	})
	r.log.Info("broker status updated", "result", result)
	if err != nil {
		return mctrl.Result{}, fmt.Errorf("error updating broker status: %w", err)
	}

	return mctrl.Result{}, nil
}

func updateBrokerStatus(broker *operatorv1alpha1.Broker, deployment *appsv1.Deployment) {
	expectedReplicas := int32(1)
	if broker.Spec.Replicas != nil {
		expectedReplicas = *broker.Spec.Replicas
	}
	availableCond := metav1.Condition{
		Type:               ConditionTypeAvailable,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             "DeploymentNotAvailable",
		Message:            fmt.Sprintf("Deployment is not yet available, %d out of %d replicas available", deployment.Status.AvailableReplicas, expectedReplicas),
	}

	if deployment.Status.AvailableReplicas >= expectedReplicas {
		availableCond.Status = metav1.ConditionTrue
		availableCond.Reason = "DeploymentAvailable"
		availableCond.Message = "Deployment is available and replicas are ready"
	}
	meta.SetStatusCondition(&broker.Status.Conditions, availableCond)

	progressingCond := metav1.Condition{
		Type:               ConditionTypeProgressing,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             "DeploymentStable",
		Message:            "Deployment is stable",
	}

	if deployment.Status.ObservedGeneration != deployment.Generation {
		progressingCond.Status = metav1.ConditionTrue
		progressingCond.Reason = "DeploymentUpdating"
		progressingCond.Message = "Deployment is updating"
	}
	meta.SetStatusCondition(&broker.Status.Conditions, progressingCond)

	degradedCond := metav1.Condition{
		Type:               ConditionTypeDegraded,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             "DeploymentHealthy",
		Message:            "All replicas are available",
	}

	if deployment.Status.UnavailableReplicas > 0 {
		degradedCond.Status = metav1.ConditionTrue
		degradedCond.Reason = "ReplicasUnavailable"
		degradedCond.Message = fmt.Sprintf("%d replicas are unavailable", deployment.Status.UnavailableReplicas)
	}
	meta.SetStatusCondition(&broker.Status.Conditions, degradedCond)
}
