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

// Package stagingworkspace implements two reconcilers that together manage
// per-consumer x provider staging kcp workspaces. For each
// [brokerv1alpha1.StagingWorkspace] object:
//
// The staging-workspace reconciler:
//  1. Creates a kcp Workspace under the configured tree-root workspace.
//  2. Waits for it to be ready and sets Status.WorkspaceURL.
//
// The staging-apibinding reconciler (triggered once WorkspaceURL is set):
//  3. Creates an APIBinding in that workspace pointing to the provider's APIExport.
//  4. Creates RBAC in that workspace so the broker user can access the bound resources.
//  5. Registers the resulting cluster in the Output provider and sets Phase=Ready.
package stagingworkspace

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	corev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	"sigs.k8s.io/multicluster-runtime/providers/clusters"

	brokerv1alpha1 "github.com/platform-mesh/resource-broker/api/broker/v1alpha1"
)

const (
	stagingWorkspaceFinalizer = "broker.platform-mesh.io/staging-workspace-finalizer"

	// APIBindingName is the name used for the APIBinding created inside each
	// staging workspace. A fixed name is fine because each staging workspace
	// binds exactly one APIExport.
	APIBindingName = "staging"

	// StagingClusterLabelKey is the label key on StagingWorkspace objects that
	// holds the cluster name used to register the staging cluster in the Output
	// provider.
	StagingClusterLabelKey = "broker.platform-mesh.io/staging-cluster-name"

	// ResourceFinalizerPrefix is the prefix for per-resource finalizers added to
	// StagingWorkspace objects. The staging-workspace reconciler deletes the SW
	// once all such finalizers have been removed by the broker.
	ResourceFinalizerPrefix = "broker.platform-mesh.io/resource-"

	// ResourceTrackedAnnotation is set on a StagingWorkspace when the first
	// resource is tracked. The staging-workspace reconciler uses it to
	// distinguish a SW that was never used from one that has had all its
	// resources removed and should be deleted.
	ResourceTrackedAnnotation = "broker.platform-mesh.io/resource-tracked"
)

// Options configures the staging workspace reconciler.
type Options struct {
	// TreeRootConfig is the REST config for the KCP tree-root workspace under
	// which staging workspaces are created (e.g. root:rb).
	TreeRootConfig *rest.Config

	// Scheme is used when building cluster.Cluster objects from workspace URLs.
	Scheme *runtime.Scheme

	// Output is the clusters provider into which ready staging clusters are
	// registered. Typically this is wired into the multi provider under the
	// broker.ProviderPrefix key.
	Output *clusters.Provider
}

func (o *Options) validate() error {
	if o.TreeRootConfig == nil {
		return fmt.Errorf("TreeRootConfig is required")
	}
	if o.Scheme == nil {
		return fmt.Errorf("scheme is required")
	}
	if o.Output == nil {
		return fmt.Errorf("output is required")
	}
	return nil
}

// Reconciler manages the lifecycle of staging KCP workspaces.
type Reconciler struct {
	client.Client
	opts Options

	// treeRootScheme is a scheme that knows about KCP tenancy, APIs, and RBAC types.
	treeRootScheme *runtime.Scheme
	// brokerUser is the CN of the certificate used by the broker to authenticate
	// against KCP. It is granted access to bound resources in each staging workspace.
	brokerUser string

	// treeRootClient is a cached client for the tree-root workspace (constant config).
	treeRootClient client.Client

	// clientCacheMu guards clientCache.
	clientCacheMu sync.Mutex
	// clientCache caches per-host clients (staging and provider workspaces).
	clientCache map[string]client.Client
}

// New creates a new staging workspace reconciler.
func New(opts Options) (*Reconciler, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	brokerUser, err := certCNFromRestConfig(opts.TreeRootConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to derive broker user from TreeRootConfig cert: %w", err)
	}

	treeRootScheme := runtime.NewScheme()
	if err := kcptenancyv1alpha1.AddToScheme(treeRootScheme); err != nil {
		return nil, fmt.Errorf("unable to add tenancy v1alpha1 to scheme: %w", err)
	}
	if err := kcpapisv1alpha2.AddToScheme(treeRootScheme); err != nil {
		return nil, fmt.Errorf("unable to add apis v1alpha2 to scheme: %w", err)
	}
	if err := rbacv1.AddToScheme(treeRootScheme); err != nil {
		return nil, fmt.Errorf("unable to add rbac v1 to scheme: %w", err)
	}

	treeRootClient, err := client.New(opts.TreeRootConfig, client.Options{Scheme: treeRootScheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create tree-root client: %w", err)
	}

	return &Reconciler{
		opts:           opts,
		treeRootScheme: treeRootScheme,
		brokerUser:     brokerUser,
		treeRootClient: treeRootClient,
		clientCache:    make(map[string]client.Client),
	}, nil
}

// certCNFromRestConfig extracts the Common Name from the client certificate in cfg.
func certCNFromRestConfig(cfg *rest.Config) (string, error) {
	certData := cfg.CertData
	if len(certData) == 0 && cfg.CertFile != "" {
		var err error
		certData, err = os.ReadFile(cfg.CertFile)
		if err != nil {
			return "", fmt.Errorf("failed to read cert file %q: %w", cfg.CertFile, err)
		}
	}
	if len(certData) == 0 {
		return "", fmt.Errorf("no client certificate found in REST config")
	}
	block, _ := pem.Decode(certData)
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block from client cert")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse client certificate: %w", err)
	}
	return cert.Subject.CommonName, nil
}

// SetupWithManager registers both controllers with a controller-runtime manager.
// The manager must have the broker v1alpha1 scheme registered.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Client = mgr.GetClient()

	if err := ctrl.NewControllerManagedBy(mgr).
		Named("staging-workspace").
		For(&brokerv1alpha1.StagingWorkspace{}).
		Complete(reconcile.Func(r.reconcileWorkspace)); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("staging-apibinding").
		For(&brokerv1alpha1.StagingWorkspace{}).
		Complete(reconcile.Func(r.reconcileAPIBinding))
}

// reconcileWorkspace manages the kcp Workspace lifecycle for a StagingWorkspace:
// it creates the workspace, waits for it to be ready, and sets Status.WorkspaceURL.
func (r *Reconciler) reconcileWorkspace(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx).WithValues("stagingWorkspace", req.NamespacedName)

	sw := &brokerv1alpha1.StagingWorkspace{}
	if err := r.Get(ctx, req.NamespacedName, sw); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !sw.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalize(ctx, sw)
	}

	if controllerutil.AddFinalizer(sw, stagingWorkspaceFinalizer) {
		if err := r.Update(ctx, sw); err != nil {
			return ctrl.Result{}, err
		}
	}

	// If the SW was previously tracking resources (ResourceTrackedAnnotation is
	// set) and all resource finalizers have since been removed, the broker is
	// done with it. First move to Terminating so the broker stops routing new
	// CRs here, then on the next reconcile trigger the actual deletion.
	if sw.Status.Phase == brokerv1alpha1.StagingWorkspacePhaseReady &&
		sw.Annotations[ResourceTrackedAnnotation] == "true" {
		hasResourceFinalizer := false
		for _, f := range sw.Finalizers {
			if strings.HasPrefix(f, ResourceFinalizerPrefix) {
				hasResourceFinalizer = true
				break
			}
		}
		if !hasResourceFinalizer {
			log.Info("No resource finalizers remain, marking staging workspace as Terminating", "stagingWorkspace", sw.Name)
			sw.Status.Phase = brokerv1alpha1.StagingWorkspacePhaseTerminating
			if err := r.Client.Status().Update(ctx, sw); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// Perform actual deletion once Terminating phase is set.
	if sw.Status.Phase == brokerv1alpha1.StagingWorkspacePhaseTerminating {
		log.Info("Deleting staging workspace in Terminating phase", "stagingWorkspace", sw.Name)
		if err := r.Delete(ctx, sw); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("failed to delete terminating staging workspace: %w", err)
		}
		return ctrl.Result{}, nil
	}

	result, err := r.ensureWorkspace(ctx, sw)
	if err != nil {
		log.Error(err, "Failed to ensure staging workspace")
		sw.Status.Phase = brokerv1alpha1.StagingWorkspacePhaseFailed
		_ = r.Client.Status().Update(ctx, sw)
	}
	return result, err
}

// reconcileAPIBinding manages the APIBinding lifecycle for a StagingWorkspace.
// It only acts once Status.WorkspaceURL is set by reconcileWorkspace.
func (r *Reconciler) reconcileAPIBinding(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx).WithValues("stagingWorkspace", req.NamespacedName)

	sw := &brokerv1alpha1.StagingWorkspace{}
	if err := r.Get(ctx, req.NamespacedName, sw); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if sw.DeletionTimestamp != nil || sw.Status.WorkspaceURL == "" {
		return ctrl.Result{}, nil
	}
	if sw.Status.Phase == brokerv1alpha1.StagingWorkspacePhaseReady {
		// After a broker restart the in-memory Output provider is empty even
		// though the StagingWorkspace is already Ready. Re-register the cluster
		// if it is missing so that the broker can route traffic again.
		stagingClusterName := sw.Labels[StagingClusterLabelKey]
		if stagingClusterName != "" {
			if _, err := r.opts.Output.Get(ctx, multicluster.ClusterName(stagingClusterName)); err == nil {
				return ctrl.Result{}, nil
			}
		} else {
			return ctrl.Result{}, nil
		}
	}

	result, err := r.ensureAPIBinding(ctx, sw)
	if err != nil {
		log.Error(err, "Failed to ensure APIBinding in staging workspace")
		sw.Status.Phase = brokerv1alpha1.StagingWorkspacePhaseFailed
		_ = r.Client.Status().Update(ctx, sw)
	}
	return result, err
}

// ensureWorkspace creates the kcp Workspace and waits for it to be ready.
// Once ready, it writes Status.WorkspaceURL to signal the APIBinding reconciler.
func (r *Reconciler) ensureWorkspace(ctx context.Context, sw *brokerv1alpha1.StagingWorkspace) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	// Step 1: Ensure the kcp Workspace exists.
	workspace := &kcptenancyv1alpha1.Workspace{}
	if err := r.treeRootClient.Get(ctx, types.NamespacedName{Name: sw.Name}, workspace); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("failed to get kcp workspace %q: %w", sw.Name, err)
		}
		workspace = &kcptenancyv1alpha1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name: sw.Name,
			},
		}
		if err := r.treeRootClient.Create(ctx, workspace); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create kcp workspace %q: %w", sw.Name, err)
		}
		log.Info("Created kcp workspace", "name", sw.Name)
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	// Step 2: Wait for the workspace to be ready.
	// If the workspace is terminating, wait for it to disappear. The next
	// reconcile will hit IsNotFound and recreate it cleanly.
	if !workspace.DeletionTimestamp.IsZero() {
		log.Info("kcp workspace is terminating, waiting for deletion", "name", sw.Name)
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	if workspace.Status.Phase != corev1alpha1.LogicalClusterPhaseReady {
		log.Info("Waiting for kcp workspace to be ready",
			"name", sw.Name,
			"phase", workspace.Status.Phase,
		)
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	workspaceURL := workspace.Spec.URL
	if workspaceURL == "" {
		log.Info("kcp workspace has no URL yet, waiting", "name", sw.Name)
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	// Signal the APIBinding reconciler by setting WorkspaceURL.
	if sw.Status.WorkspaceURL != workspaceURL {
		sw.Status.WorkspaceURL = workspaceURL
		if err := r.Client.Status().Update(ctx, sw); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// ensureAPIBinding creates the APIBinding in the staging workspace, waits for
// it to be bound, ensures broker RBAC, and registers the cluster in Output.
func (r *Reconciler) ensureAPIBinding(ctx context.Context, sw *brokerv1alpha1.StagingWorkspace) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	// Build a client pointing directly at the staging workspace.
	stagingHost, err := substituteClusterPath(r.opts.TreeRootConfig.Host, sw.Status.WorkspaceURL)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to derive staging workspace host: %w", err)
	}
	stagingClient, err := r.cachedClient(stagingHost)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create staging workspace client: %w", err)
	}

	// Step 3: Ensure the APIBinding exists in the staging workspace.
	binding := &kcpapisv1alpha2.APIBinding{}
	err = stagingClient.Get(ctx, types.NamespacedName{Name: APIBindingName}, binding)
	if apierrors.IsNotFound(err) {
		// Fetch the provider's APIExport to mirror its permissionClaims into the
		// APIBinding. This allows the provider's service accounts (e.g. api-syncagent)
		// to create namespaces, secrets, and events in the staging workspace.
		permClaims, err := r.providerPermissionClaims(ctx, sw.Spec.ProviderPath, sw.Spec.APIExportName)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to fetch provider APIExport permission claims: %w", err)
		}
		binding = &kcpapisv1alpha2.APIBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: APIBindingName,
			},
			Spec: kcpapisv1alpha2.APIBindingSpec{
				Reference: kcpapisv1alpha2.BindingReference{
					Export: &kcpapisv1alpha2.ExportBindingReference{
						Path: sw.Spec.ProviderPath,
						Name: sw.Spec.APIExportName,
					},
				},
				PermissionClaims: permClaims,
			},
		}
		if err := stagingClient.Create(ctx, binding); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create APIBinding in staging workspace: %w", err)
		}
		log.Info("Created APIBinding in staging workspace", "workspace", sw.Name,
			"permissionClaims", len(permClaims))
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get APIBinding in staging workspace: %w", err)
	}

	// Step 4: Wait for the APIBinding to be bound.
	if binding.Status.Phase != kcpapisv1alpha2.APIBindingPhaseBound {
		log.Info("Waiting for APIBinding to be bound",
			"workspace", sw.Name,
			"phase", binding.Status.Phase,
		)
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	// Step 4.5: Ensure the broker user has RBAC to access the bound resources.
	// In kcp, resources introduced by an APIBinding require explicit RBAC even for
	// workspace admins when no maximalPermissionPolicy is set on the APIExport.
	if err := r.ensureBrokerRBAC(ctx, stagingClient, binding, sw.Name); err != nil {
		return ctrl.Result{}, err
	}

	// Step 5: Register the staging cluster in the Output provider.
	stagingClusterName := sw.Labels[StagingClusterLabelKey]
	if stagingClusterName == "" {
		return ctrl.Result{}, fmt.Errorf("StagingWorkspace %q is missing label %q", sw.Name, StagingClusterLabelKey)
	}

	// Only create and register a new cluster.Cluster if this workspace is not
	// already known to the output provider — cluster.New starts informers and
	// is expensive.
	if _, err := r.opts.Output.Get(ctx, multicluster.ClusterName(stagingClusterName)); err != nil {
		stagingCfg := rest.CopyConfig(r.opts.TreeRootConfig)
		stagingCfg.Host = stagingHost
		stagingCluster, err := cluster.New(stagingCfg, func(o *cluster.Options) {
			o.Scheme = r.opts.Scheme
		})
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create staging cluster: %w", err)
		}
		if err := r.opts.Output.Add(ctx, multicluster.ClusterName(stagingClusterName), stagingCluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add staging cluster to output: %w", err)
		}
		log.Info("Registered staging cluster in output provider",
			"workspace", sw.Name,
			"clusterName", stagingClusterName,
		)
	}

	// Mark the StagingWorkspace as fully ready.
	sw.Status.Phase = brokerv1alpha1.StagingWorkspacePhaseReady
	if err := r.Client.Status().Update(ctx, sw); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// finalize removes the staging cluster from the Output provider and deletes
// the KCP workspace before clearing the finalizer.
func (r *Reconciler) finalize(ctx context.Context, sw *brokerv1alpha1.StagingWorkspace) error {
	log := ctrllog.FromContext(ctx).WithValues("stagingWorkspace", sw.Name)

	stagingClusterName := sw.Labels[StagingClusterLabelKey]
	if stagingClusterName != "" {
		r.opts.Output.Remove(multicluster.ClusterName(stagingClusterName))
		log.Info("Removed staging cluster from output provider", "clusterName", stagingClusterName)
	}

	// Delete the KCP workspace (ignore NotFound in case it was already deleted).
	workspace := &kcptenancyv1alpha1.Workspace{}
	if err := r.treeRootClient.Get(ctx, types.NamespacedName{Name: sw.Name}, workspace); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to get KCP workspace during finalization: %w", err)
		}
	} else if workspace.DeletionTimestamp.IsZero() {
		if err := r.treeRootClient.Delete(ctx, workspace); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete KCP workspace: %w", err)
		}
		log.Info("Deleted KCP workspace", "name", sw.Name)
	}

	if controllerutil.RemoveFinalizer(sw, stagingWorkspaceFinalizer) {
		if err := r.Update(ctx, sw); err != nil {
			return err
		}
	}

	return nil
}

// providerPermissionClaims fetches the APIExport from the provider workspace and
// returns its permissionClaims converted to AcceptablePermissionClaims (all Accepted,
// matchAll selector). Returns nil if the APIExport has no claims.
func (r *Reconciler) providerPermissionClaims(
	ctx context.Context,
	providerPath string,
	exportName string,
) ([]kcpapisv1alpha2.AcceptablePermissionClaim, error) {
	providerHost, err := clusterHost(r.opts.TreeRootConfig.Host, providerPath)
	if err != nil {
		return nil, fmt.Errorf("failed to derive provider host for %q: %w", providerPath, err)
	}
	providerClient, err := r.cachedClient(providerHost)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider client: %w", err)
	}

	export := &kcpapisv1alpha2.APIExport{}
	if err := providerClient.Get(ctx, types.NamespacedName{Name: exportName}, export); err != nil {
		return nil, fmt.Errorf("failed to get APIExport %q from provider %q: %w", exportName, providerPath, err)
	}

	if len(export.Spec.PermissionClaims) == 0 {
		return nil, nil
	}

	claims := make([]kcpapisv1alpha2.AcceptablePermissionClaim, 0, len(export.Spec.PermissionClaims))
	for _, pc := range export.Spec.PermissionClaims {
		claims = append(claims, kcpapisv1alpha2.AcceptablePermissionClaim{
			ScopedPermissionClaim: kcpapisv1alpha2.ScopedPermissionClaim{
				PermissionClaim: pc,
				Selector: kcpapisv1alpha2.PermissionClaimSelector{
					MatchAll: true,
				},
			},
			State: kcpapisv1alpha2.ClaimAccepted,
		})
	}
	return claims, nil
}

// clusterHost builds a direct KCP URL for the given workspace path by replacing
// the /clusters/... suffix in baseHost with /clusters/<path>.
func clusterHost(baseHost, clusterPath string) (string, error) {
	u, err := url.Parse(baseHost)
	if err != nil {
		return "", fmt.Errorf("failed to parse base host %q: %w", baseHost, err)
	}
	idx := strings.Index(u.Path, "/clusters/")
	if idx >= 0 {
		u.Path = u.Path[:idx]
	}
	u.Path = "/clusters/" + clusterPath
	return u.String(), nil
}

// ensureBrokerRBAC creates a ClusterRole and ClusterRoleBinding in the staging
// workspace so that the broker user (r.brokerUser) can read/write the resources
// that were bound via the APIBinding. KCP does not automatically grant bound-resource
// access to workspace admins, so we create explicit RBAC.
func (r *Reconciler) ensureBrokerRBAC(
	ctx context.Context,
	stagingClient client.Client,
	binding *kcpapisv1alpha2.APIBinding,
	workspaceName string,
) error {
	log := ctrllog.FromContext(ctx)

	const roleName = "broker-binding-access"

	rules := make([]rbacv1.PolicyRule, 0, len(binding.Status.BoundResources))
	for _, br := range binding.Status.BoundResources {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{br.Group},
			Resources: []string{br.Resource},
			Verbs:     []string{"*"},
		})
	}

	role := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: roleName}}
	if _, err := controllerutil.CreateOrUpdate(ctx, stagingClient, role, func() error {
		role.Rules = rules
		return nil
	}); err != nil {
		return fmt.Errorf("failed to reconcile broker ClusterRole in staging workspace: %w", err)
	}
	log.Info("Reconciled broker ClusterRole in staging workspace", "workspace", workspaceName)

	crb := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: roleName}}
	if _, err := controllerutil.CreateOrUpdate(ctx, stagingClient, crb, func() error {
		crb.RoleRef = rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     roleName,
		}
		crb.Subjects = []rbacv1.Subject{{
			Kind:     "User",
			APIGroup: "rbac.authorization.k8s.io",
			Name:     r.brokerUser,
		}}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to reconcile broker ClusterRoleBinding in staging workspace: %w", err)
	}
	log.Info("Reconciled broker ClusterRoleBinding in staging workspace",
		"workspace", workspaceName, "user", r.brokerUser)

	return nil
}

// cachedClient returns a cached client for the given host, creating one if needed.
// The config's credentials are taken from TreeRootConfig; only the Host differs.
func (r *Reconciler) cachedClient(host string) (client.Client, error) {
	r.clientCacheMu.Lock()
	defer r.clientCacheMu.Unlock()
	if cl, ok := r.clientCache[host]; ok {
		return cl, nil
	}
	cfg := rest.CopyConfig(r.opts.TreeRootConfig)
	cfg.Host = host
	cl, err := client.New(cfg, client.Options{Scheme: r.treeRootScheme})
	if err != nil {
		return nil, err
	}
	r.clientCache[host] = cl
	return cl, nil
}

// substituteClusterPath builds a workspace URL that uses the same base host
// (scheme, host, port) as baseHost but with the /clusters/<path> suffix taken
// from workspaceURL. This is needed because KCP's Workspace.Spec.URL may point
// to a different server endpoint (e.g. front-proxy) than the kubeconfig URL.
func substituteClusterPath(baseHost, workspaceURL string) (string, error) {
	base, err := url.Parse(baseHost)
	if err != nil {
		return "", fmt.Errorf("failed to parse base host %q: %w", baseHost, err)
	}
	ws, err := url.Parse(workspaceURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse workspace URL %q: %w", workspaceURL, err)
	}
	idx := strings.Index(ws.Path, "/clusters/")
	if idx < 0 {
		return "", fmt.Errorf("workspace URL %q does not contain /clusters/ path segment", workspaceURL)
	}
	base.Path = ws.Path[idx:]
	return base.String(), nil
}
