# Brokering Certificates

This example uses resource-broker to broker Certificate resources from
a consumer cluster to matching provider clusters.

## Overview

The resource-broker runs in the platform cluster and connects to the
consumer and provider clusters directly.

The Certificate resources in the provider clusters are backed by
cert-manager and kro.

## Prerequisites

### Required Tools

- docker
- kind
- kubectl
- helm
- yq
- go

## Components

### Consumer Cluster

The consumer cluster has the `Certificate` CRD installed.

### Provider Clusters (InternalCA & ExternalCA)

Each provider cluster runs kro and cert-manager to back the `Certificate` resource with self-signed issuers.

They each configure an `AcceptAPI` resource to declare under which constraints they will be able to serve `Certificate` resources from consumers:

```yaml
apiVersion: broker.platform-mesh.io/v1alpha1
kind: AcceptAPI
metadata:
  name: certificates.example.platform-mesh.io
spec:
  # which resource this AcceptAPI applies to
  # a provider can have multiple AcceptAPIs for the same resource with
  # different filters
  gvr:
    group: example.platform-mesh.io
    version: v1alpha1
    resource: certificates
  filters:
    # Filter on FQDN suffix
    - key: fqdn
      suffix: internal.corp
```

The resource-broker caches these and uses them to route requests.

## Running the Example

<!--
```bash ci
if [[ -n "$CI" ]]; then
    _ci() {
        ./examples/certs/run.bash ci
    }
    trap _ci EXIT
fi
```
-->

### Setup

1. Setup the kind clusters and install components

```bash ci
./examples/certs/run.bash setup
```

2. Build and start the resource-broker in the platform cluster

```bash ci
./examples/certs/run.bash start-broker
```

### Example

In this example we will request a Certificate in the consumer cluster.
The resource-broker will pick it up and pass the request to one of the
providers and sync back the resulting Secret.

```mermaid
sequenceDiagram
    participant Consumer as Consumer Cluster
    participant Broker as Resource Broker<br/>(Platform)
    participant InternalCA as InternalCA Provider
    participant ExternalCA as ExternalCA Provider

    Note over Consumer,ExternalCA: Phase 1: Initial Certificate Request

    Consumer->>Consumer: Create Certificate<br/>fqdn: app.internal.corp
    Broker->>Consumer: Watch detects new Certificate
    Broker->>InternalCA: Match AcceptAPI filter<br/>(suffix: internal.corp) ✓
    Broker->>ExternalCA: Match AcceptAPI filter<br/>(suffix: corp.com) ✗
    Broker->>InternalCA: Create Certificate
    InternalCA->>InternalCA: kro creates cert-manager Certificate
    InternalCA->>InternalCA: cert-manager issues certificate
    InternalCA->>InternalCA: Secret created<br/>(tls.crt, tls.key)
    InternalCA-->>Broker: Certificate Ready<br/>+ relatedResources.secret
    Broker->>Consumer: Sync Secret<br/>CN: app.internal.corp

    Note over Consumer,ExternalCA: Phase 2: Migration to External CA

    Consumer->>Consumer: Patch Certificate<br/>fqdn: app.corp.com
    Broker->>Consumer: Watch detects Certificate update
    Broker->>InternalCA: Match AcceptAPI filter<br/>(suffix: internal.corp) ✗
    Broker->>ExternalCA: Match AcceptAPI filter<br/>(suffix: corp.com) ✓
    Broker->>ExternalCA: Create Certificate
    ExternalCA->>ExternalCA: kro creates cert-manager Certificate
    ExternalCA->>ExternalCA: cert-manager issues certificate
    ExternalCA->>ExternalCA: Secret created<br/>(tls.crt, tls.key)
    ExternalCA-->>Broker: Certificate Ready<br/>+ relatedResources.secret
    Broker->>Consumer: Sync Secret<br/>CN: app.corp.com
    Broker->>InternalCA: Delete Certificate
    InternalCA->>InternalCA: Cleanup resources
```

Create the certificate in the consumer cluster:

```bash ci
kubectl --kubeconfig ./kubeconfigs/consumer.kubeconfig apply -f ./examples/certs/consumer/cert.yaml
```

<!--
```bash ci
source ./hack/lib.bash
kubectl::wait ./kubeconfigs/internalca.kubeconfig certificates.example.platform-mesh.io/cert-from-consumer default create
kubectl::wait ./kubeconfigs/internalca.kubeconfig certificates.cert-manager.io/cert-from-consumer default create
kubectl::wait ./kubeconfigs/internalca.kubeconfig certificates.cert-manager.io/cert-from-consumer default condition=Ready
```
-->

This will be picked up by resource-broker and sent to the InternalCA provider:

```bash ci
kubectl --kubeconfig ./kubeconfigs/internalca.kubeconfig get certificates.example.platform-mesh.io cert-from-consumer -o yaml
```

```yaml
apiVersion: example.platform-mesh.io/v1alpha1
kind: Certificate
metadata:
  # ...
  finalizers:
  - broker.platform-mesh.io/generic-finalizer
  - kro.run/finalizer
  # ...
  name: cert-from-consumer
  namespace: default
  # ...
spec:
  fqdn: app.internal.corp
status:
  # ...
  relatedResources:
    secret:
      gvk:
        group: core
        kind: Secret
        version: v1
      name: cert-from-consumer
      namespace: default
  state: ACTIVE
  status: Available
```

Which KRO will back with a cert-manager Certificate and Secret:

```bash ci
kubectl --kubeconfig ./kubeconfigs/internalca.kubeconfig get certificates,secrets -l kro.run/owned=true -o yaml
```

```yaml
apiVersion: v1
items:
- apiVersion: cert-manager.io/v1
  kind: Certificate
  metadata:
    # ...
    name: cert-from-consumer
    namespace: default
    # ...
  spec:
    commonName: app.internal.corp
    issuerRef:
      group: cert-manager.io
      kind: ClusterIssuer
      name: selfsigned-cluster-issuer
    privateKey:
      algorithm: ECDSA
      size: 256
    secretName: cert-from-consumer
  status:
    # ...
- apiVersion: v1
  data:
    ca.crt: # ...
    tls.crt:  # ...
    tls.key: # ...
  kind: Secret
  metadata:
    # ...
    name: cert-from-consumer
    namespace: default
    ownerReferences:
    - apiVersion: cert-manager.io/v1
      blockOwnerDeletion: true
      controller: true
      kind: Certificate
      name: cert-from-consumer
      uid: f6010fea-e5b6-4be7-b6c3-b57165dc2588
    # ...
  type: kubernetes.io/tls
kind: List
metadata:
  resourceVersion: ""
```

And the resource-broker synchronizes it back to the consumer cluster:

<!--
```bash ci
kubectl::wait ./kubeconfigs/consumer.kubeconfig secrets/cert-from-consumer default create
```
-->

```bash ci
kubectl --kubeconfig ./kubeconfigs/consumer.kubeconfig get secrets cert-from-consumer -o yaml
```

```yaml
apiVersion: v1
data:
  ca.crt: # ...
  tls.crt: # ...
  tls.key: # ...
kind: Secret
metadata:
  # ...
  name: cert-from-consumer
  namespace: default
  ownerReferences:
  - apiVersion: example.platform-mesh.io/v1alpha1
    kind: Certificate
    name: cert-from-consumer
  # ...
type: kubernetes.io/tls
```

Getting the certificate from the secret will show the expected FQDN:

<!--
```bash ci
kubectl::wait::cert::subject ./kubeconfigs/consumer.kubeconfig "cert-from-consumer" default "app.internal.corp"
```
-->

```bash ci
kubectl --kubeconfig ./kubeconfigs/consumer.kubeconfig get secret cert-from-consumer -o jsonpath='{.data.tls\.crt}' | base64 --decode | openssl x509 -noout -subject
```

```text
subject=CN=app.internal.corp
```

Now, update the Certificate in the consumer cluster to use a different FQDN:

```bash ci
kubectl --kubeconfig ./kubeconfigs/consumer.kubeconfig patch certificate cert-from-consumer --type merge -p '{"spec":{"fqdn":"app.corp.com"}}'
```

<!--
```bash ci
kubectl::wait ./kubeconfigs/externalca.kubeconfig certificates.example.platform-mesh.io/cert-from-consumer default create
kubectl::wait ./kubeconfigs/externalca.kubeconfig certificates.cert-manager.io/cert-from-consumer default create
kubectl::wait ./kubeconfigs/externalca.kubeconfig certificates.cert-manager.io/cert-from-consumer default condition=Ready
```
-->

resource-broker will first create the Certificate in the ExternalCA provider:

<!--
```bash ci
kubectl::wait ./kubeconfigs/externalca.kubeconfig certificates.cert-manager.io/cert-from-consumer default create
kubectl::wait ./kubeconfigs/externalca.kubeconfig certificates.cert-manager.io/cert-from-consumer default condition=Ready
```
-->

```bash ci
kubectl --kubeconfig ./kubeconfigs/externalca.kubeconfig get certificates.example.platform-mesh.io,certificates,secrets -l kro.run/owned=true -o yaml
```

```yaml
apiVersion: v1
items:
# The generic Certificate in the ExternalCA provider
- apiVersion: example.platform-mesh.io/v1alpha1
  kind: Certificate
  metadata:
    # ...
    finalizers:
    - broker.platform-mesh.io/generic-finalizer
    - kro.run/finalizer
    # ..
    name: cert-from-consumer
    namespace: default
    # ..
  spec:
    fqdn: app.corp.com
  status:
    # ...
    relatedResources:
      secret:
        gvk:
          group: core
          kind: Secret
          version: v1
        name: cert-from-consumer
        namespace: default
    state: ACTIVE
    status: Available
# The resulting cert-manager Certificate
- apiVersion: cert-manager.io/v1
  kind: Certificate
  metadata:
    # ...
    name: cert-from-consumer
    namespace: default
    # ..
  spec:
    commonName: app.corp.com
    issuerRef:
      group: cert-manager.io
      kind: ClusterIssuer
      name: selfsigned-cluster-issuer
    privateKey:
      algorithm: ECDSA
      size: 256
    secretName: cert-from-consumer
  status:
    # ...
# And the secret generated by cert-manager
- apiVersion: v1
  data:
    ca.crt: # ...
    tls.crt: # ...
    tls.key: # ...
  kind: Secret
  metadata:
    # ...
    name: cert-from-consumer
    namespace: default
    ownerReferences:
    - apiVersion: cert-manager.io/v1
      blockOwnerDeletion: true
      controller: true
      kind: Certificate
      name: cert-from-consumer
    # ...
  type: kubernetes.io/tls
kind: List
metadata:
  resourceVersion: ""
```

And then delete it from the InternalCA provider:

<!--
```bash ci
kubectl::wait ./kubeconfigs/internalca.kubeconfig certificates.example.platform-mesh.io/cert-from-consumer default delete
```
-->

```bash ci
kubectl --kubeconfig ./kubeconfigs/internalca.kubeconfig get certificates.example.platform-mesh.io,certificates,secrets -l kro.run/owned=true
```

And the updated certificate will reflect the new FQDN:

<!--
```bash ci
kubectl::wait::cert::subject ./kubeconfigs/consumer.kubeconfig "cert-from-consumer" default "app.corp.com"
```
-->

```bash ci
kubectl --kubeconfig ./kubeconfigs/consumer.kubeconfig get secret cert-from-consumer -o jsonpath='{.data.tls\.crt}' | base64 --decode | openssl x509 -noout -subject
```

```text
subject=CN=app.corp.com
```

### Cleanup

4. (Optional) Clean up resources created during the example

```bash
./examples/certs/run.bash cleanup
./examples/certs/run.bash stop-broker
```

Or delete the clusters:

```bash
kind delete cluster --name broker-platform
kind delete cluster --name broker-consumer
kind delete cluster --name broker-internalca
kind delete cluster --name broker-externalca
```
