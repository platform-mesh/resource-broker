# platform-mesh

This example deploys resource-broker as a provider in the [Platform Mesh](https://platform-mesh.io/).

Loosely following the instructions in the provider quickstart: https://github.com/platform-mesh/provider-quickstart

> [!NOTE]
> At present the guide is only targeting the local setup of Platform Mesh.
> The guide will be updated in the future to include other deployment options.

## Preparation

Set two environment variables - one for the Platform Mesh instance and
one for the compute cluster:

```bash
cp ../helm-charts/.secret/kcp/admin.kubeconfig kcp-admin.kubeconfig
export PM_KUBECONFIG="$(realpath kcp-admin.kubeconfig)"
kind export kubeconfig --name platform-mesh --kubeconfig compute.kubeconfig
export COMPUTE_KUBECONFIG="$(realpath compute.kubeconfig)"
```

The `PM_KUBECONFIG` will be created when we set up the Platform Mesh
organisation and account later.

### Platform Mesh

This setup needs a running Platform Mesh instance. The easiest way to
get one is to deploy the [local setup](https://github.com/platform-mesh/helm-charts/tree/main/local-setup).

### Build and load images

Build the resource-broker-operator and resource-broker-kcp images and
load them into the kind cluster:

```bash ci
export IMG_KCP=resource-broker-kcp:platform-mesh
export IMG_OPERATOR=resource-broker-operator:platform-mesh
export IMG_PORTAL=resource-broker-portal:platform-mesh

make docker-build-kcp docker-build-operator docker-build-portal
make kind-load-kcp kind-load-operator kind-load-portal KIND_CLUSTER=platform-mesh
```

### Deploy operator

Deploy the operator and its resources into the kind cluster:

```bash ci
make deploy-operator KUBECONFIG="$COMPUTE_KUBECONFIG"
```

### Deploy portal

Deploy the portal into the kind cluster:

```bash ci
make deploy-pm-portal KUBECONFIG="$COMPUTE_KUBECONFIG"
```

## resource-broker setup in Platform Mesh

### Workspace

<!--
Why do users see all organisations? Should that be the case?
-->

Create layout:

```bash
KUBECONFIG="$PM_KUBECONFIG" kubectl ws :root
KUBECONFIG="$PM_KUBECONFIG" kubectl create-workspace --type root:providers --enter providers --ignore-existing
KUBECONFIG="$PM_KUBECONFIG" kubectl create-workspace --type root:provider --enter resource-broker --ignore-existing
```

<!--
Users shouldn't have different passwords for different organisations,
that is very confusing.
TODO: check if it wouldn't be enough to create a "normal" account workspace with the root:provider type
-->


<!--
The following is how I think this should work. Above is the current setup.

Create an organisation and an account in Platform Mesh. For the local
setup in this example we are using the organisation
`resource-broker-org` and the account `resource-broker`.

Download the kubeconfig for the account and set that as the
`PM_KUBECONFIG`.

```bash
export PM_KUBECONFIG=path/to/pm.kubeconfig
```

export PM_KUBECONFIG="$(realpath pm.kubeconfig)"

export PM_KUBECONFIG="$(realpath pm-provider.kubeconfig)"


KUBECONFIG="$PM_KUBECONFIG" kubectl ws :root
KUBECONFIG="$PM_KUBECONFIG" kubectl create-workspace --enter providers
KUBECONFIG="$PM_KUBECONFIG" kubectl create-workspace --enter resource-broker
-->

### Resources

Setup the APIResourceSchema. For this example we'll be using the `Certificate` example.

Create the APIResourcheSchema for Certificate and AcceptAPI:

```bash
make kcp-snapshot-apply PM_KUBECONFIG="$PM_KUBECONFIG"
```


Kustomize the APIExports, RBAC and Platform Mesh resources:

```bash
KUBECONFIG="$PM_KUBECONFIG" kubectl apply -k ./examples/platform-mesh/root:providers:resource-broker
```

Now the Certificate will show up in the marketplace and users can bind it.

### Start the broker

```bash ci
./examples/platform-mesh/run.bash start-broker "$COMPUTE_KUBECONFIG" "$PM_KUBECONFIG"
```


## Provider setup

Setup the providers backing the certificate:

```bash
./examples/platform-mesh/run.bash setup-provider internalca internal.corp "$PM_KUBECONFIG" "$COMPUTE_KUBECONFIG"
./examples/platform-mesh/run.bash setup-provider externalca corp.com "$PM_KUBECONFIG" "$COMPUTE_KUBECONFIG"
```

<!--
TODO register content configurations etcpp for internalca/externalca?
-->

## Consumer

Create a consumer org and account and bind the brokered APIs into it. If
the stuff doesn't show in the UI just use kubectl to create
a certificate in the account workspace.

```bash
kubectl apply -f ./examples/platform-mesh/cert.yaml
```

