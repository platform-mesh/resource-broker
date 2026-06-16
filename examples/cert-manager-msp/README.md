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

## Running the Example

### Setup

This msp example expects basic knowledge from the kcp-certs example.

**Step 1 — Bring up the platform and the empty compute cluster**

Creates the `broker-platform` and `broker-cert-manager` kind clusters,
installs cert-manager / etcd-druid / kcp / kro, deploys
resource-broker-operator, bootstraps kcp, and creates the platform workspace
with the `acceptapis` and `certificates` APIExports.

```bash ci
./examples/cert-manager-msp/run.bash setup-platform
```

**Step 2 — Apply compute manifests (ClusterIssuer + KRO RGD)**

- **`issuer.yaml`** — self-signed `ClusterIssuer` that cert-manager uses to
  sign certificates.
- **`rgd.yaml`** — KRO `ResourceGraphDefinition` that registers the
  `certmanager.ca/Certificate` CRD and translates it into a
  `cert-manager.io/Certificate`.

We wait for the RGD to become `Ready`, which confirms KRO has registered the CRD.

```bash ci
kubectl --kubeconfig kubeconfigs/cert-manager.kubeconfig \
  apply -k ./examples/platform-mesh/cert-manager
kubectl --kubeconfig kubeconfigs/cert-manager.kubeconfig \
  wait --for=condition=Ready rgd/certificates.certmanager.ca --timeout=120s
```

**Step 3 — Wire up the cert-manager provider**

Creates the `root:cert-manager` workspace and its `certificates` APIExport,
deploys api-syncagent on the compute cluster, applies the
`PublishedResource`, binds and applies the `AcceptAPI`, creates the VW
kubeconfig that resource-broker uses to write into the provider workspace,
and creates the consumer workspace.

```bash ci
./examples/cert-manager-msp/run.bash setup-provider
```

**Step 4 — Start resource-broker**

Builds and loads the resource-broker image, mounts the operator kubeconfig
into the broker, and applies the `Broker` resource configured to watch
`identity.generic.platform-mesh.io/Certificate`.

```bash ci
./examples/cert-manager-msp/run.bash start-broker
```

### Example

**Step 5 — Consumer binds the Certificate API**

The consumer workspace binds the `certificates` APIExport from the platform
workspace, making `identity.generic.platform-mesh.io/Certificate` available
to the tenant.

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig apply -f- <<EOF
apiVersion: apis.kcp.io/v1alpha2
kind: APIBinding
metadata:
  name: certificates
spec:
  reference:
    export:
      path: root:platform
      name: certificates
  permissionClaims:
    - resource: secrets
      group: ''
      state: Accepted
      verbs: ['*']
      selector:
        matchAll: true
    - resource: events
      group: ''
      state: Accepted
      verbs: ['*']
      selector:
        matchAll: true
    - resource: namespaces
      group: ''
      state: Accepted
      verbs: ['*']
      selector:
        matchAll: true
EOF
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
  wait --for=condition=Ready apibinding/certificates --timeout=60s
```

**Step 6 — Consumer orders a Certificate**

The consumer creates a `Certificate` with just the `fqdn` they need.
resource-broker routes it to the cert-manager provider, KRO translates it
into a cert-manager Certificate, cert-manager signs it, and api-syncagent
syncs the TLS Secret back.

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

**Step 7 — Verify**

Wait for the TLS Secret to appear in the consumer workspace and confirm the
certificate was issued for the right FQDN.

```bash ci
kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
  wait secret/cert-from-consumer --for=create --timeout=5m

kubectl --kubeconfig kubeconfigs/workspaces/consumer.kubeconfig \
  get secret cert-from-consumer \
  -o jsonpath="{.data.tls\.crt}" | base64 -d | openssl x509 -noout -subject
# Expected: subject=CN=my-app.platform-mesh.io
```

### Cleanup

Removes the consumer's Certificate and APIBinding so step 5 onwards can be
re-run against the same platform / provider setup.

```bash noci
./examples/cert-manager-msp/run.bash cleanup
```

To tear down the whole environment, delete the kind clusters:

```bash noci
kind delete cluster --name broker-platform
kind delete cluster --name broker-cert-manager
```
