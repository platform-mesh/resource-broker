#!/usr/bin/env bash

# cd into repo root
example_dir="$(dirname "$0")"
cd "$(dirname "$0")/../.."
source "./hack/lib.bash"

kubeconfigs="$PWD/kubeconfigs"
log "Using directory for kubeconfigs: $kubeconfigs"

kind_platform="$kubeconfigs/platform.kubeconfig"
kind_consumer="$kubeconfigs/consumer.kubeconfig"
kind_internalca="$kubeconfigs/internalca.kubeconfig"
kind_externalca="$kubeconfigs/externalca.kubeconfig"

_setup() {
    log "Setting up platform cluster"
    kind::cluster platform "$kind_platform"
    # Platform only needs the Migration and MigrationConfiguration CRDs but
    # mcmanager currently requires all to be installed (see #132)
    kubectl::apply "$kind_platform" \
        ./config/broker/crd/broker.platform-mesh.io_acceptapis.yaml \
        ./config/broker/crd/broker.platform-mesh.io_migrationconfigurations.yaml \
        ./config/broker/crd/broker.platform-mesh.io_migrations.yaml \
        ./config/example/crd/example.platform-mesh.io_certificates.yaml

    log "Setting up provider internalca"
    kind::cluster internalca "$kind_internalca"
    helm::install::kro "$kind_internalca"
    helm::install::certmanager "$kind_internalca"
    kubectl::kustomize "$kind_internalca" "$example_dir/internalca"

    log "Setting up provider externalca"
    kind::cluster externalca "$kind_externalca"
    helm::install::kro "$kind_externalca"
    helm::install::certmanager "$kind_externalca"
    kubectl::kustomize "$kind_externalca" "$example_dir/externalca"

    log "Setting up consumer consumer"
    kind::cluster consumer "$kind_consumer"
    kubectl::kustomize "$kind_consumer" "$example_dir/consumer"
}

_start_broker() {
    log "Starting broker"

    if [[ -z "$CI" ]]; then
        make docker-build docker-build-operator || die "Failed to build docker images"
    fi

    make kind-load kind-load-operator KIND_CLUSTER=broker-platform \
        || die "Failed to load images into kind cluster"
    make deploy-operator KUBECONFIG="$kind_platform" || die "Failed to deploy resource-broker-operator"

    kubectl::kubeconfig::secret "$kind_platform" "$kind_platform" platform resource-broker-system broker-platform-control-plane:6443
    kubectl::kubeconfig::secret "$kind_platform" "$kind_consumer" consumer resource-broker-system broker-consumer-control-plane:6443
    kubectl::kubeconfig::secret "$kind_platform" "$kind_internalca" internalca resource-broker-system broker-internalca-control-plane:6443
    kubectl::kubeconfig::secret "$kind_platform" "$kind_externalca" externalca resource-broker-system broker-externalca-control-plane:6443

    kubectl::kustomize "$kind_platform" "$example_dir/platform"
    kubectl::wait "$kind_platform" broker/resource-broker resource-broker-system condition=Available
}

_stop_broker() {
    kubectl --kubeconfig "$kind_platform" delete -n resource-broker-system broker/resource-broker
}

_cleanup() {
    kubectl::delete "$kind_consumer" \
        certificates.example.platform-mesh.io/cert-from-consumer \
        secret/cert-from-consumer

    kubectl::delete "$kind_internalca" \
        certificates.example.platform-mesh.io/cert-from-consumer \
        certificates.cert-manager.io/cert-from-consumer \
        secret/cert-from-consumer

    kubectl::delete "$kind_externalca" \
        certificates.example.platform-mesh.io/cert-from-consumer \
        certificates.cert-manager.io/cert-from-consumer \
        secret/cert-from-consumer
    return 0
}

_ci() {
    kubectl --kubeconfig "$kind_platform" logs -n resource-broker-system deployment/resource-broker-operator > resource-broker-operator.log
    kubectl --kubeconfig "$kind_platform" logs -n resource-broker-system deployment/resource-broker > resource-broker.log
    kubectl --kubeconfig "$kind_consumer" get certificates.example.platform-mesh.io -A -o yaml > consumer-certificates.yaml

    kubectl --kubeconfig "$kind_internalca" logs deployment/cert-manager > internalca-cert-manager.log
    kubectl --kubeconfig "$kind_internalca" get certificates.example.platform-mesh.io -A -o yaml > internalca-certificates.yaml

    kubectl --kubeconfig "$kind_externalca" logs deployment/cert-manager > externalca-cert-manager.log
    kubectl --kubeconfig "$kind_externalca" get certificates.example.platform-mesh.io -A -o yaml > externalca-certificates.yaml
}

case "$1" in
    (setup) _setup ;;
    (cleanup) _cleanup ;;
    (start-broker) _start_broker ;;
    (stop-broker) _stop_broker ;;
    (ci) _ci;;
    (*) die "Unknown command: $1" ;;
esac
