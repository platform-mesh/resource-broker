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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-runtime/pkg/log"

	mctrl "sigs.k8s.io/multicluster-runtime"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
)

// GenericReconciler reconciles the specified GVK.
type GenericReconciler struct {
	GVK schema.GroupVersionKind
}

// SetupWithManager sets up the controller with the Manager.
func (r *GenericReconciler) SetupWithManager(mgr mctrl.Manager) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(r.GVK)
	return mcbuilder.ControllerManagedBy(mgr).
		Named("generic").
		For(obj).
		Complete(r)
}

// Reconcile triggers the lifecycling of watched objects.
func (r *GenericReconciler) Reconcile(ctx context.Context, _ mctrl.Request) (mctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here

	return mctrl.Result{}, nil
}
