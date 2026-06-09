# cert-manager MSP Provider

This example wires up cert-manager as a Managed Service Provider for the
resource-broker generic Certificate API, using
[api-syncagent](https://docs.kcp.io/api-syncagent/main/) to bridge the
compute cluster and kcp, and [kro](https://kro.run) to translate the generic
`Certificate{fqdn}` into a real `cert-manager.io/Certificate`.

No custom operator is written. The entire provider is declarative YAML.

## Overview

A consumer creates an `identity.generic.platform-mesh.io/Certificate` in their
kcp workspace. resource-broker sees it through the platform APIExport's virtual
workspace, matches it against the cert-manager provider's `AcceptAPI`, and routes
it to the provider workspace. api-syncagent picks it up, creates a
`certmanager.ca/Certificate` on the compute cluster, kro translates that into a
real cert-manager Certificate, cert-manager signs it, and api-syncagent syncs the
resulting TLS Secret back to the consumer workspace.

```
Consumer workspace
  └─ Certificate{fqdn}
        │  resource-broker routes via AcceptAPI
        ▼
Provider workspace (root:cert-manager)
        │  api-syncagent watches + syncs down
        ▼
Compute cluster
  ├─ certmanager.ca/Certificate  (KRO input)
  ├─ cert-manager.io/Certificate (KRO output → cert-manager)
  └─ Secret (TLS)
        │  api-syncagent syncs back
        ▼
Consumer workspace
  └─ Secret cert-from-consumer (tls.crt, tls.key, ca.crt)
```

## Components

### Platform Cluster

Runs kcp and resource-broker. Hosts the `root:platform` workspace which
exports the generic `identity.generic.platform-mesh.io/Certificate` API for
consumers and the `acceptapis` API for providers.

### Provider Workspace (`root:cert-manager`)

The cert-manager provider's kcp workspace. api-syncagent fills in the
`certificates` APIExport schema automatically. The `AcceptAPI` resource declares
this provider can handle all `identity.generic.platform-mesh.io/certificates`.

### Compute Cluster (`broker-cert-manager`)

Runs cert-manager, kro, and api-syncagent. Our manifests under
`examples/platform-mesh/cert-manager/` are applied here.

### Consumer Workspace (`root:consumer`)

Represents an end-user or tenant. Binds the `certificates` APIExport from the
platform workspace and creates Certificate resources.

## Provider Manifests

The provider manifests live under `examples/platform-mesh/`:

| File | Purpose |
|---|---|
| `cert-manager/issuer.yaml` | Self-signed `ClusterIssuer` on the compute cluster |
| `cert-manager/rgd.yaml` | KRO `ResourceGraphDefinition` — translates `certmanager.ca/Certificate{fqdn}` → `cert-manager.io/Certificate` |
| `cert-manager/publishedresource.yaml` | api-syncagent config — publishes local cert as `identity.generic.platform-mesh.io/Certificate` in kcp, syncs Secret back |
| `root:providers:cert-manager/apiexport.yaml` | Minimal `APIExport` (api-syncagent fills in the schema) |
| `root:providers:cert-manager/acceptapi.yaml` | Declares this provider handles `identity.generic.platform-mesh.io/certificates` |
| `root:providers:cert-manager/bind-acceptapis.yaml` | Binds the `acceptapis` CRD from the platform workspace |

## Prerequisites

- `docker`
- `kind`
- `kubectl`
- `helm`
- `yq`
- `go`

<!--
```bash ci
source ./hack/lib.bash
mkdir -p kubeconfigs/workspaces
kcp::setup::plugins
```
-->

## Running the Example

### Setup

<!--
```bash ci
if [[ -z "$CI" ]]; then
    make docker-build-operator || die "Failed to build resource-broker-operator docker image"
    make docker-build-kcp || die "Failed to build resource-broker-kcp docker image"
fi
```
-->

**Step 1 — Fix the vendor directory**

Regenerates `vendor/modules.txt` if it has drifted from `go.mod`. No internet needed — modules are already in the Go cache.

```bash ci
go mod vendor
```

**Step 2 — Create the platform cluster**

Hosts kcp and resource-broker.

```bash ci
kind::cluster platform kubeconfigs/platform.kubeconfig
kind export kubeconfig --name broker-platform --kubeconfig kubeconfigs/platform.kubeconfig
```

**Step 3 — Install platform components**

- **cert-manager** — kcp needs it to issue its own TLS certificates.
- **etcd-druid** — manages the etcd cluster that kcp uses as its storage backend.
- **kcp** — the multi-workspace API server.

```bash ci
helm::install::certmanager kubeconfigs/platform.kubeconfig
helm::install::etcddruid kubeconfigs/platform.kubeconfig
helm::install::kcp kubeconfigs/platform.kubeconfig
```

**Step 4 — Deploy resource-broker-operator**

Manages the broker deployment lifecycle. Loads the pre-built image into kind (no registry needed) and applies its manifests.

```bash ci
make kind-load-operator KIND_CLUSTER=broker-platform
make deploy-operator KUBECONFIG=kubeconfigs/platform.kubeconfig
```

**Step 5 — Bootstrap kcp and create the platform workspace**

Applies the kcp bootstrap manifests (etcd cluster, front-proxy, root shard), waits for kcp to be ready, exports admin kubeconfigs, and creates `root:platform` — the workspace where the broker lives and all APIExports are published.

```bash ci
kubectl::kustomize kubeconfigs/platform.kubeconfig ./examples/kcp-certs/platform
kcp::setup::kubeconfigs \
  kubeconfigs/platform.kubeconfig \
  kubeconfigs/kcp-admin.kubeconfig \
  kubeconfigs/kcp-from-host.kubeconfig
kcp::create_workspace \
  kubeconfigs/kcp-admin.kubeconfig \
  kubeconfigs/workspaces/platform.kubeconfig \
  platform
```

**Step 6 — Install broker CRDs and create APIExports in the platform workspace**

- Installs broker CRDs (`StagingWorkspace`, `Migration`, etc.) so the broker can persist its state inside kcp.
- Creates the **`acceptapis` APIExport** — providers bind this to declare which APIs they can serve.
- Creates the **`certificates` APIExport** for `identity.generic.platform-mesh.io/Certificate` — the generic API consumers bind to order certificates, independent of which provider backs it.

```bash ci
kubectl::apply kubeconfigs/workspaces/platform.kubeconfig \
  ./config/broker/crd/broker.platform-mesh.io_migrationconfigurations.yaml \
  ./config/broker/crd/broker.platform-mesh.io_migrations.yaml \
  ./config/broker/crd/broker.platform-mesh.io_stagingworkspaces.yaml

kcp::apiexport kubeconfigs/workspaces/platform.kubeconfig \
  ./config/broker/crd/broker.platform-mesh.io_acceptapis.yaml \
  secrets get,list,watch

kcp::apiexport kubeconfigs/workspaces/platform.kubeconfig \
  ./config/generic/crd/identity.generic.platform-mesh.io_certificates.yaml \
  secrets '*' events '*' namespaces '*'
```

**Step 7 — Create the cert-manager compute cluster**

The compute cluster is where the actual work happens — cert-manager signs certificates here and kro translates the generic `Certificate{fqdn}` into a real `cert-manager Certificate`. Completely separate from the platform cluster.

```bash ci
kind::cluster cert-manager kubeconfigs/cert-manager.kubeconfig
kind export kubeconfig --name broker-cert-manager --kubeconfig kubeconfigs/cert-manager.kubeconfig
helm::install::certmanager kubeconfigs/cert-manager.kubeconfig
helm::install::kro kubeconfigs/cert-manager.kubeconfig
```

**Step 8 — Apply compute manifests (ClusterIssuer + KRO RGD)**

- **`issuer.yaml`** — self-signed `ClusterIssuer` that cert-manager uses to sign certificates.
- **`rgd.yaml`** — KRO `ResourceGraphDefinition` that registers the `certmanager.ca/Certificate` CRD and translates it into a `cert-manager.io/Certificate`.

We wait for the RGD to become `Ready`, which confirms KRO has registered the CRD.

```bash ci
kubectl::kustomize kubeconfigs/cert-manager.kubeconfig ./examples/platform-mesh/cert-manager
kubectl::wait kubeconfigs/cert-manager.kubeconfig rgd/certificates.certmanager.ca "" condition=Ready
```

**Step 9 — Create the cert-manager kcp workspace and a minimal APIExport**

The provider workspace (`root:cert-manager`) is where the cert-manager provider lives inside kcp. The `certificates` APIExport is created with an empty spec — api-syncagent fills in the schema automatically when it starts.

```bash ci
kcp::create_workspace \
  kubeconfigs/kcp-admin.kubeconfig \
  kubeconfigs/workspaces/cert-manager.kubeconfig \
  cert-manager

kubectl --kubeconfig kubeconfigs/workspaces/cert-manager.kubeconfig \
  apply -f examples/platform-mesh/root:providers:cert-manager/apiexport.yaml
```

**Step 10 — Deploy api-syncagent on the compute cluster**

api-syncagent bridges the compute cluster and kcp:
1. Watches the provider kcp workspace for `identity.generic.platform-mesh.io/Certificate` objects routed by resource-broker.
2. Creates corresponding `certmanager.ca/Certificate` objects on the compute cluster (picked up by KRO → cert-manager).
3. Syncs the resulting TLS `Secret` back up to the consumer workspace.

A service account is created in the provider workspace, a kubeconfig is generated for it, stored as a secret on the compute cluster, then api-syncagent is deployed via Helm pointing at that secret.

```bash ci
api_syncagent_token="$(kcp::serviceaccount::admin \
  kubeconfigs/workspaces/cert-manager.kubeconfig api-syncagent default)"
kubeconfig::create::token kubeconfigs/api-syncagent-cert-manager.kubeconfig \
  "$(kubectl::kubeconfig::current_server_url kubeconfigs/workspaces/cert-manager.kubeconfig)" \
  "$api_syncagent_token"
kubectl::kubeconfig::secret kubeconfigs/cert-manager.kubeconfig \
  kubeconfigs/api-syncagent-cert-manager.kubeconfig \
  cert-manager default broker-platform-control-plane:32111

helm::install::api_syncagent kubeconfigs/cert-manager.kubeconfig \
  certificates cert-manager kubeconfig-cert-manager --set replicas=1
```

**Step 11 — Apply the PublishedResource**

`publishedresource.yaml` tells api-syncagent:
- **Local resource**: `certmanager.ca/Certificate` on the compute cluster.
- **Projected as**: `identity.generic.platform-mesh.io/Certificate` in kcp.
- **Related resource**: the TLS `Secret` — api-syncagent syncs this back to the consumer workspace.

```bash ci
kubectl --kubeconfig kubeconfigs/cert-manager.kubeconfig \
  apply -f examples/platform-mesh/cert-manager/publishedresource.yaml
```

**Step 12 — Wire up the provider workspace**

Wait for api-syncagent to update the `certificates` APIExport with the `certmanager.ca` schema projected as `identity.generic.platform-mesh.io`. Then:
- **Bind `acceptapis`** from the platform workspace — gives the provider workspace the `AcceptAPI` CRD.
- **Apply `acceptapi.yaml`** — declares this provider handles all `identity.generic.platform-mesh.io/certificates`. The `secret-name` annotation tells resource-broker which kubeconfig to use for routing.
- **Bind the local `certificates` APIExport** back into the provider workspace so `identity.generic.platform-mesh.io/Certificate` is available locally for api-syncagent and resource-broker.

```bash ci
until kubectl --kubeconfig kubeconfigs/workspaces/cert-manager.kubeconfig \
  get apiexport certificates -o jsonpath='{.spec.resources}' 2>/dev/null \
  | grep -q "identity"; do sleep 3; done

kubectl --kubeconfig kubeconfigs/workspaces/cert-manager.kubeconfig apply -f- <<EOF
apiVersion: apis.kcp.io/v1alpha2
kind: APIBinding
metadata:
  name: acceptapis
spec:
  reference:
    export:
      path: root:platform
      name: acceptapis
  permissionClaims:
    - resource: secrets
      group: ''
      state: Accepted
      verbs: ['get','list','watch']
      selector:
        matchAll: true
EOF
kubectl --kubeconfig kubeconfigs/workspaces/cert-manager.kubeconfig \
  wait --for=condition=Ready apibinding/acceptapis --timeout=60s

kubectl --kubeconfig kubeconfigs/workspaces/cert-manager.kubeconfig \
  apply -f examples/platform-mesh/root:providers:cert-manager/acceptapi.yaml

kcp::apibinding kubeconfigs/workspaces/cert-manager.kubeconfig "root:cert-manager" certificates \
  secrets "" '*' events "" '*' namespaces "" '*'
```

**Step 13 — Create the VW kubeconfig for resource-broker and the consumer workspace**

resource-broker needs a kubeconfig pointing at the provider's Virtual Workspace endpoint to write routed certificates into the provider workspace. Stored as a secret so the broker finds it via the `secret-name` annotation on the `AcceptAPI`.

`root:consumer` is the workspace representing an end-user or tenant.

```bash ci
cluster_id=$(kubectl --kubeconfig kubeconfigs/workspaces/cert-manager.kubeconfig \
  get apiexportendpointslices/certificates \
  -o jsonpath='{.metadata.annotations.kcp\.io/cluster}')
endpoint_url="https://broker-platform-control-plane:32443/services/apiexport/$cluster_id/certificates/clusters/$cluster_id"
sa_token="$(kcp::serviceaccount::admin \
  kubeconfigs/workspaces/cert-manager.kubeconfig broker default)"
kubeconfig::create::token \
  kubeconfigs/workspaces/cert-manager.vw.kubeconfig "$endpoint_url" "$sa_token"
kubectl::kubeconfig::secret kubeconfigs/workspaces/cert-manager.kubeconfig \
  kubeconfigs/workspaces/cert-manager.vw.kubeconfig \
  cert-manager default broker-platform-control-plane:32443

kcp::create_workspace \
  kubeconfigs/kcp-admin.kubeconfig \
  kubeconfigs/workspaces/consumer.kubeconfig \
  consumer
```

**Step 14 — Build and start resource-broker**

resource-broker watches consumer workspaces for `identity.generic.platform-mesh.io/Certificate` objects, finds a matching provider via `AcceptAPI`, and routes the certificate to that provider's virtual workspace. The `watch-kind` is set to `identity.generic.platform-mesh.io` via `sed`.

```bash ci
make kind-load-kcp KIND_CLUSTER=broker-platform

kubectl --kubeconfig kubeconfigs/platform.kubeconfig \
  get secret operator-kubeconfig \
  -o jsonpath='{.data.kubeconfig}' | base64 -d > kubeconfigs/operator.kubeconfig
yq -i '(.clusters[] | select(.name=="default") | .cluster.server) += ":platform"' \
  kubeconfigs/operator.kubeconfig

kubectl create secret generic kcp-kubeconfig \
  --namespace=resource-broker-system \
  --dry-run=client -o yaml \
  --from-file=kubeconfig=kubeconfigs/operator.kubeconfig \
  | kubectl --kubeconfig kubeconfigs/platform.kubeconfig apply -f-

cat examples/kcp-certs/platform/broker.yaml \
  | sed 's|Certificate.v1alpha1.example.platform-mesh.io|Certificate.v1alpha1.identity.generic.platform-mesh.io|' \
  | kubectl --kubeconfig kubeconfigs/platform.kubeconfig apply -f-

kubectl::wait kubeconfigs/platform.kubeconfig \
  broker/resource-broker resource-broker-system condition=Available
```

### Example

**Step 15 — Consumer binds the Certificate API**

The consumer workspace binds the `certificates` APIExport from the platform workspace, making `identity.generic.platform-mesh.io/Certificate` available to the tenant.

```bash ci
kcp::apibinding kubeconfigs/workspaces/consumer.kubeconfig "root:platform" certificates \
  secrets "" '*' events "" '*' namespaces "" '*'
```

**Step 16 — Consumer orders a Certificate**

The consumer creates a `Certificate` with just the `fqdn` they need. resource-broker routes it to the cert-manager provider, KRO translates it into a cert-manager Certificate, cert-manager signs it, and api-syncagent syncs the TLS Secret back.

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig apply -f- <<EOF
apiVersion: identity.generic.platform-mesh.io/v1alpha1
kind: Certificate
metadata:
  name: cert-from-consumer
  namespace: default
spec:
  fqdn: my-app.platform-mesh.io
EOF
```

**Step 17 — Verify**

Wait for the TLS Secret to appear in the consumer workspace and confirm the certificate was issued for the right FQDN.

<!--
```bash ci
kubectl::wait::cert::subject \
  kubeconfigs/workspaces/consumer.kubeconfig \
  "cert-from-consumer" \
  "default" \
  "my-app.platform-mesh.io"
```
-->

```bash
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
  wait secret/cert-from-consumer --for=create --timeout=5m

kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
  get secret cert-from-consumer \
  -o jsonpath="{.data.tls\.crt}" | base64 -d | openssl x509 -noout -subject
# Expected: subject=CN=my-app.platform-mesh.io
```

### Cleanup

```bash noci
for c in broker-platform broker-cert-manager; do
  kind delete cluster --name "$c" 2>/dev/null || true
done
```
