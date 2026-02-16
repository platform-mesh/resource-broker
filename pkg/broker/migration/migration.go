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
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcluster "sigs.k8s.io/controller-runtime/pkg/cluster"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	mctrl "sigs.k8s.io/multicluster-runtime"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
	"github.com/platform-mesh/resource-broker/pkg/sync"
)

const (
	migrationStageLabel = "broker.platform-mesh.io/migration-stage"
	migrationIDLabel    = "broker.platform-mesh.io/migration-id"
)

// MigrationOptions holds the options for the migration reconciler.
type MigrationOptions struct { //nolint:revive
	ControllerNamePrefix      string
	Compute                   ctrlclient.Client
	GetCoordinationCluster    func(context.Context, string) (ctrlcluster.Cluster, error)
	GetProviderCluster        func(context.Context, string) (ctrlcluster.Cluster, error)
	GetMigrationConfiguration func(metav1.GroupVersionKind, metav1.GroupVersionKind) (brokerv1alpha1.MigrationConfiguration, bool)
}

type migrationReconciler struct {
	opts MigrationOptions
}

// SetupController creates a controller to handle MigrationConfiguration resources.
func SetupController(mgr mctrl.Manager, opts MigrationOptions) error {
	r := &migrationReconciler{
		opts: opts,
	}

	return mctrl.NewControllerManagedBy(mgr).
		Named(opts.ControllerNamePrefix + "-migration").
		For(&brokerv1alpha1.Migration{}).
		Complete(r)
}

func (mr *migrationReconciler) Reconcile(ctx context.Context, req mctrl.Request) (mctrl.Result, error) {
	log := ctrllog.FromContext(ctx).WithValues(
		"clusterName", req.ClusterName,
		"name", req.Name,
		"namespace", req.Namespace,
	)
	log.Info("Reconciling migration")

	ctx = ctrllog.IntoContext(ctx, log)

	cluster, err := mr.opts.GetCoordinationCluster(ctx, req.ClusterName)
	if err != nil {
		log.Error(err, "Failed to get cluster")
		return ctrl.Result{}, err
	}

	migration, err := mr.getMigration(ctx, req.NamespacedName, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	log.Info("Migration found")

	switch migration.Status.State {
	case brokerv1alpha1.MigrationStateUnknown:
		log.Info("Setting migration state to Pending")
		// Not using the updateStatus helper, the migration was just
		// retrieved and the .ID in status needs to be set.
		migration.Status.ID = strings.ToLower(rand.Text())
		migration.Status.State = brokerv1alpha1.MigrationStatePending
		if err := cluster.GetClient().Status().Update(ctx, migration); err != nil {
			log.Error(err, "Failed to update migration status")
			return ctrl.Result{}, err
		}

	case brokerv1alpha1.MigrationStateCutoverCompleted:
		log.Info("Migration already completed, skipping")
		return ctrl.Result{}, nil
	}

	log.Info("Copying related resources from source and target clusters")
	if err := mr.copyRelatedResources(ctx, migration.Spec.From, "from-"); err != nil {
		return ctrl.Result{}, err
	}

	if err := mr.copyRelatedResources(ctx, migration.Spec.To, "to-"); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Fetching migration configuration")
	migrationConfig, ok := mr.opts.GetMigrationConfiguration(migration.Spec.From.GVK, migration.Spec.To.GVK)
	if !ok {
		log.Info("No migration configuration found for migration", "fromGVK", migration.Spec.From.GVK, "toGVK", migration.Spec.To.GVK)
		return ctrl.Result{}, nil
	}

	if len(migrationConfig.Spec.Stages) == 0 {
		log.Info("Migration configuration has no stages defined, marking migration as completed")
		return mctrl.Result{}, mr.updateStatus(ctx, req.NamespacedName, cluster, func(migration *brokerv1alpha1.Migration) {
			migration.Status.State = brokerv1alpha1.MigrationStateCutoverCompleted
		})
	}

	stageIndex := 0
	for i, stage := range migrationConfig.Spec.Stages {
		if migration.Status.Stage == stage.Name {
			stageIndex = i
			break
		}
	}

	if stageIndex < 0 || stageIndex >= len(migrationConfig.Spec.Stages) {
		return ctrl.Result{}, fmt.Errorf("current migration stage %s not found in migration configuration", migration.Status.Stage)
	}

	log.Info("Processing migration stage", "stage", migrationConfig.Spec.Stages[stageIndex].Name)

	curStage := migrationConfig.Spec.Stages[stageIndex]
	log = log.WithValues("migrationStage", curStage.Name)

	stageResources, err := mr.deployStage(ctx, migration.Status.ID, curStage)
	if err != nil {
		log.Error(err, "Failed to deploy resources for migration stage", "stage", curStage.Name)
		return ctrl.Result{}, err
	}

	// Set next state if pending, otherwise there is not transition out
	// of the initial state.
	if migration.Status.State == brokerv1alpha1.MigrationStatePending {
		if err := mr.updateStatus(ctx, req.NamespacedName, cluster, func(migration *brokerv1alpha1.Migration) {
			migration.Status.State = brokerv1alpha1.MigrationStateInitialInProgress
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	log.Info("Deployed resources for migration stage", "resources", stageResources)

	log.Info("Evaluating success conditions for migration stage")
	success, err := mr.checkSuccessConditions(ctx, stageResources, curStage.SuccessConditions)
	if err != nil {
		log.Error(err, "Failed to evaluate success conditions for migration stage")
		return ctrl.Result{}, err
	}
	if !success {
		log.Info("Not all success conditions met for migration stage, skipping")
		return ctrl.Result{}, nil
	}

	log.Info("All success conditions met for migration stage", "stage", curStage.Name)
	for _, res := range stageResources {
		if err := mr.opts.Compute.Delete(ctx, res); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to clean up resource %s/%s from migration stage %s: %w", res.GetNamespace(), res.GetName(), curStage.Name, err)
		}
	}

	if stageIndex+1 >= len(migrationConfig.Spec.Stages) {
		log.Info("All migration stages completed, marking migration as completed")
		return ctrl.Result{}, mr.updateStatus(ctx, req.NamespacedName, cluster, func(migration *brokerv1alpha1.Migration) {
			migration.Status.State = brokerv1alpha1.MigrationStateCutoverCompleted
		})
	}

	if curStage.Progress {
		log.Info("Progressing migration state to next phase")
		if err := mr.updateStatus(ctx, req.NamespacedName, cluster, func(migration *brokerv1alpha1.Migration) {
			switch migration.Status.State {
			case brokerv1alpha1.MigrationStateInitialInProgress:
				migration.Status.State = brokerv1alpha1.MigrationStateInitialCompleted
			case brokerv1alpha1.MigrationStateCutoverInProgress:
				migration.Status.State = brokerv1alpha1.MigrationStateCutoverCompleted
			}
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	// requeue to process the next stage
	return ctrl.Result{Requeue: true}, nil
}

func (mr *migrationReconciler) getMigration(ctx context.Context, nn types.NamespacedName, cluster ctrlcluster.Cluster) (*brokerv1alpha1.Migration, error) {
	migration := &brokerv1alpha1.Migration{}
	if err := cluster.GetClient().Get(ctx, nn, migration); err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to get migration")
		return nil, err
	}
	return migration, nil
}

func (mr *migrationReconciler) updateStatus(ctx context.Context, nn types.NamespacedName, cl ctrlcluster.Cluster, updateFunc func(*brokerv1alpha1.Migration)) error {
	migration, err := mr.getMigration(ctx, nn, cl)
	if err != nil {
		return err
	}

	updateFunc(migration)

	if err := cl.GetClient().Status().Update(ctx, migration); err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to update migration status")
		return err
	}
	return nil
}

func (mr *migrationReconciler) copyRelatedResources(ctx context.Context, from brokerv1alpha1.MigrationRef, prefix string) error {
	cl, err := mr.opts.GetProviderCluster(ctx, from.ClusterName)
	if err != nil {
		return err
	}

	log := ctrllog.FromContext(ctx)

	relatedResources, err := sync.CollectRelatedResources(
		ctx,
		cl.GetClient(),
		schema.GroupVersionKind{
			Group:   from.GVK.Group,
			Version: from.GVK.Version,
			Kind:    from.GVK.Kind,
		},
		types.NamespacedName{
			Name:      from.Name,
			Namespace: from.Namespace,
		},
	)
	if err != nil {
		log.Error(err, "Failed to collect related resources")
		return err
	}

	var errs error
	for key, rr := range relatedResources {
		if err := mr.copyRelatedResource(ctx, cl, prefix, rr); err != nil {
			log.Error(err, "Failed to copy related resource", "prefix", prefix, "key", key)
			errs = errors.Join(errs, err)
		}
	}

	return errs
}

func (mr *migrationReconciler) copyRelatedResource(ctx context.Context, source ctrlcluster.Cluster, prefix string, relatedResource brokerv1alpha1.RelatedResource) error {
	log := ctrllog.FromContext(ctx).WithValues(
		"relatedResourceNamespace", relatedResource.Namespace,
		"relatedResourceName", relatedResource.Name,
		"relatedResourceGVK", relatedResource.GVK,
	)

	sourceObj := &unstructured.Unstructured{}
	sourceObj.SetGroupVersionKind(
		schema.GroupVersionKind{
			Group:   relatedResource.GVK.Group,
			Version: relatedResource.GVK.Version,
			Kind:    relatedResource.GVK.Kind,
		},
	)
	sourceNamespacedName := types.NamespacedName{
		Name:      relatedResource.Name,
		Namespace: relatedResource.Namespace,
	}
	if err := source.GetClient().Get(ctx, sourceNamespacedName, sourceObj); err != nil {
		log.Error(err, "Failed to get related resource from source cluster")
		return err
	}

	targetObj := sync.StripClusterMetadata(sourceObj)
	targetObj.SetName(prefix + relatedResource.Name)
	if relatedResource.Namespace != "" {
		targetObj.SetNamespace(relatedResource.Namespace) // TODO handle namespacing. all resources should probably be in the same namespace
	}

	existingObj := &unstructured.Unstructured{}
	existingObj.SetGroupVersionKind(targetObj.GroupVersionKind())
	err := mr.opts.Compute.Get(
		ctx,
		ctrlclient.ObjectKeyFromObject(targetObj),
		existingObj,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Creating related resource in coordination cluster")
			if err := mr.opts.Compute.Create(ctx, targetObj); err != nil {
				log.Error(err, "Failed to create related resource in coordination cluster")
				return err
			}
			return nil
		}
		log.Error(err, "Failed to get existing related resource in coordination cluster")
		return err
	}

	targetObj.SetResourceVersion(existingObj.GetResourceVersion())
	if err := mr.opts.Compute.Update(ctx, targetObj); err != nil {
		log.Error(err, "Failed to update related resource in coordination cluster")
		return err
	}
	return nil
}

func (mr *migrationReconciler) deployStage(ctx context.Context, migrationID string, stage brokerv1alpha1.MigrationStage) (map[string]*unstructured.Unstructured, error) {
	var ret = make(map[string]*unstructured.Unstructured)
	var errs error

	for name, rawExt := range stage.Templates {
		obj := &unstructured.Unstructured{}
		if err := obj.UnmarshalJSON(rawExt.Raw); err != nil {
			errs = errors.Join(errs, fmt.Errorf("failed to unmarshal template %s: %w", name, err))
			continue
		}
		obj.SetName(fmt.Sprintf("%s-%s", migrationID, name))
		obj.SetNamespace("default") // TODO: make namespace configurable
		labels := obj.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[migrationStageLabel] = stage.Name
		labels[migrationIDLabel] = migrationID
		obj.SetLabels(labels)

		existingObj := &unstructured.Unstructured{}
		existingObj.SetGroupVersionKind(obj.GroupVersionKind())
		err := mr.opts.Compute.Get(
			ctx,
			types.NamespacedName{
				Name:      obj.GetName(),
				Namespace: obj.GetNamespace(),
			},
			existingObj,
		)
		if err == nil {
			ret[name] = existingObj
			continue
		}

		if !apierrors.IsNotFound(err) {
			errs = errors.Join(errs, fmt.Errorf("failed to get existing resource from template %s: %w", name, err))
			continue
		}

		if err := mr.opts.Compute.Create(ctx, obj); err != nil {
			errs = errors.Join(errs, fmt.Errorf("failed to create resource from template %s: %w", name, err))
			continue
		}
		ret[name] = obj
	}

	return ret, errs
}

func (mr *migrationReconciler) checkSuccessConditions(ctx context.Context, stageResources map[string]*unstructured.Unstructured, successConditions []string) (bool, error) {
	log := ctrllog.FromContext(ctx)

	celEnv, celArgs, err := mr.prepCEL(stageResources)
	if err != nil {
		return false, fmt.Errorf("failed to prepare CEL environment: %w", err)
	}

	for _, condExpr := range successConditions {
		ast, issues := celEnv.Compile(condExpr)
		if issues.Err() != nil {
			return false, fmt.Errorf("failed to compile CEL expression %s: %w", condExpr, issues.Err())
		}

		prg, err := celEnv.Program(ast)
		if err != nil {
			return false, fmt.Errorf("failed to create CEL program for expression %s: %w", condExpr, err)
		}

		val, _, err := prg.ContextEval(ctx, celArgs)
		if err != nil {
			return false, fmt.Errorf("failed to evaluate CEL expression %s: %w", condExpr, err)
		}

		if val.Type().TypeName() != "bool" {
			return false, fmt.Errorf("CEL expression %s did not evaluate to a boolean", condExpr)
		}

		if !val.Value().(bool) {
			log.Info("Success condition not yet met for migration stage", "condition", condExpr)
			return false, nil
		}

		log.Info("Success condition met for migration stage", "condition", condExpr)
	}

	return true, nil
}

func (mr *migrationReconciler) prepCEL(stageResources map[string]*unstructured.Unstructured) (*cel.Env, map[string]any, error) {
	envArgs := []cel.EnvOption{}
	evalArgs := make(map[string]any)
	for name, obj := range stageResources {
		envArgs = append(envArgs, cel.Variable(name, cel.DynType))
		evalArgs[name] = obj
	}

	env, err := cel.NewEnv(envArgs...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	return env, evalArgs, nil
}
