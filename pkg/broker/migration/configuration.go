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

package migration

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/cluster"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	mctrl "sigs.k8s.io/multicluster-runtime"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
)

const migrationConfigurationFinalizer = "broker.platform-mesh.io/migrationconfiguration-finalizer"

// ConfigurationOptions defines the options for the
// MigrationConfiguration reconciler.
type ConfigurationOptions struct {
	ControllerNamePrefix         string
	GetCluster                   func(context.Context, string) (cluster.Cluster, error)
	SetMigrationConfiguration    func(from metav1.GroupVersionKind, to metav1.GroupVersionKind, config brokerv1alpha1.MigrationConfiguration)
	DeleteMigrationConfiguration func(from metav1.GroupVersionKind, to metav1.GroupVersionKind)
}

type configurationReconciler struct {
	opts ConfigurationOptions
}

// SetupConfigurationController creates a controller to handle MigrationConfiguration resources.
func SetupConfigurationController(mgr mctrl.Manager, opts ConfigurationOptions) error {
	r := &configurationReconciler{
		opts: opts,
	}

	return mctrl.NewControllerManagedBy(mgr).
		Named(opts.ControllerNamePrefix + "-migration-configuration").
		For(&brokerv1alpha1.MigrationConfiguration{}).
		Complete(r)
}

func (cr *configurationReconciler) Reconcile(ctx context.Context, req mctrl.Request) (mctrl.Result, error) {
	log := ctrllog.FromContext(ctx).WithValues(
		"clusterName", req.ClusterName,
		"name", req.Name,
		"namespace", req.Namespace,
	)
	log.Info("Reconciling MigrationConfiguration")

	// TODO: This would probably be better as a handler that can be
	// attached to an indexer.

	cl, err := cr.opts.GetCluster(ctx, req.ClusterName)
	if err != nil {
		return mctrl.Result{}, err
	}

	migrationConfiguration := &brokerv1alpha1.MigrationConfiguration{}
	if err := cl.GetClient().Get(ctx, req.NamespacedName, migrationConfiguration); err != nil {
		if apierrors.IsNotFound(err) {
			return mctrl.Result{}, nil
		}
		return mctrl.Result{}, err
	}

	if !migrationConfiguration.DeletionTimestamp.IsZero() {
		cr.opts.DeleteMigrationConfiguration(
			migrationConfiguration.Spec.From,
			migrationConfiguration.Spec.To,
		)
		if ctrlutil.ContainsFinalizer(migrationConfiguration, migrationConfigurationFinalizer) {
			ctrlutil.RemoveFinalizer(migrationConfiguration, migrationConfigurationFinalizer)
			if err := cl.GetClient().Update(ctx, migrationConfiguration); err != nil {
				return mctrl.Result{}, err
			}
		}
		return mctrl.Result{}, nil
	}

	cr.opts.SetMigrationConfiguration(
		migrationConfiguration.Spec.From,
		migrationConfiguration.Spec.To,
		*migrationConfiguration,
	)

	if !ctrlutil.ContainsFinalizer(migrationConfiguration, migrationConfigurationFinalizer) {
		ctrlutil.AddFinalizer(migrationConfiguration, migrationConfigurationFinalizer)
		if err := cl.GetClient().Update(ctx, migrationConfiguration); err != nil {
			return mctrl.Result{}, err
		}
	}

	return mctrl.Result{}, nil
}
