#!/usr/bin/env bash

# cd into repo root
example_dir="$(realpath "$(dirname "$0")")"
cd "$example_dir/../.."
source "./hack/lib.bash"

if [[ -n "$CI" ]]; then
    kcp::setup::plugins
fi

kubeconfigs="$PWD/kubeconfigs"
log "Using directory for kubeconfigs: $kubeconfigs"

workspace_kubeconfigs="$kubeconfigs/workspaces"
mkdir -p "$workspace_kubeconfigs"

kind_platform="$kubeconfigs/platform.kubeconfig"
ws_platform="$workspace_kubeconfigs/platform.kubeconfig"

kind_certmanager="$kubeconfigs/cert-manager.kubeconfig"
ws_certmanager="$workspace_kubeconfigs/cert-manager.kubeconfig"
vw_certmanager="$workspace_kubeconfigs/cert-manager.vw.kubeconfig"

ws_consumer="$workspace_kubeconfigs/consumer.kubeconfig"

_setup_platform() {
    log "Setting up platform cluster"
    kind::cluster platform "$kind_platform"
    helm::install::certmanager "$kind_platform"
    helm::install::etcddruid "$kind_platform"
    helm::install::kcp "$kind_platform"

    log "Deploy resource-broker-operator"
    if [[ -z "$CI" ]]; then
        make docker-build-operator || die "Failed to build resource-broker-operator docker image"
    fi
    make kind-load-operator KIND_CLUSTER=broker-platform \
        || die "Failed to load resource-broker-operator into kind cluster"
    make deploy-operator KUBECONFIG="$kind_platform" || die "Failed to deploy resource-broker-operator"

    log "Setting up kcp"
    kubectl::kustomize "$kind_platform" "./examples/kcp-certs/platform"
    kcp::setup::kubeconfigs \
        "$kind_platform" \
        "$kubeconfigs/kcp-admin.kubeconfig" \
        "$kubeconfigs/kcp-from-host.kubeconfig"

    log "Setting up platform kcp workspace"
    kcp::create_workspace "$kubeconfigs/kcp-admin.kubeconfig" "$ws_platform" "platform"

    log "Installing broker CRDs into platform workspace"
    kubectl::apply \
        "$ws_platform" \
        ./config/broker/crd/broker.platform-mesh.io_migrationconfigurations.yaml \
        ./config/broker/crd/broker.platform-mesh.io_migrations.yaml \
        ./config/broker/crd/broker.platform-mesh.io_stagingworkspaces.yaml

    log "Setting up AcceptAPI APIExport for providers"
    kcp::apiexport "$ws_platform" ./config/broker/crd/broker.platform-mesh.io_acceptapis.yaml \
        secrets get,list,watch

    log "Setting up Certificate APIExport for consumers"
    kcp::apiexport "$ws_platform" \
        ./config/generic/crd/identity.generic.platform-mesh.io_certificates.yaml \
        secrets '*' \
        events '*' \
        namespaces '*'

    log "Setting up cert-manager compute cluster"
    kind::cluster cert-manager "$kind_certmanager"
    helm::install::certmanager "$kind_certmanager"
    helm::install::kro "$kind_certmanager"
}

_setup_provider() {
    log "Setting up cert-manager kcp workspace and APIExport"
    kcp::create_workspace "$kubeconfigs/kcp-admin.kubeconfig" "$ws_certmanager" "cert-manager"
    kubectl --kubeconfig "$ws_certmanager" \
        apply -f "$example_dir/../platform-mesh/root:providers:cert-manager/apiexport.yaml"

    log "Deploying api-syncagent on the compute cluster"
    local api_syncagent_token
    api_syncagent_token="$(kcp::serviceaccount::admin "$ws_certmanager" api-syncagent default)"
    local api_syncagent_kubeconfig="$kubeconfigs/api-syncagent-cert-manager.kubeconfig"
    kubeconfig::create::token "$api_syncagent_kubeconfig" \
        "$(kubectl::kubeconfig::current_server_url "$ws_certmanager")" \
        "$api_syncagent_token"
    kubectl::kubeconfig::secret "$kind_certmanager" "$api_syncagent_kubeconfig" \
        cert-manager default broker-platform-control-plane:32111

    helm::install::api_syncagent "$kind_certmanager" \
        certificates cert-manager kubeconfig-cert-manager --set replicas=1

    log "Applying PublishedResource"
    kubectl --kubeconfig "$kind_certmanager" \
        apply -f "$example_dir/../platform-mesh/cert-manager/publishedresource.yaml"

    log "Wiring up the cert-manager provider workspace"
    until kubectl --kubeconfig "$ws_certmanager" \
        get apiexport certificates -o jsonpath='{.spec.resources}' 2>/dev/null \
        | grep -q "identity"; do sleep 3; done

    kubectl --kubeconfig "$ws_certmanager" apply -f- <<EOF
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
    kubectl --kubeconfig "$ws_certmanager" \
        wait --for=condition=Ready apibinding/acceptapis --timeout=60s

    kubectl --kubeconfig "$ws_certmanager" \
        apply -f "$example_dir/../platform-mesh/root:providers:cert-manager/acceptapi.yaml"

    kcp::apibinding "$ws_certmanager" "root:cert-manager" certificates \
        secrets "" '*' \
        events "" '*' \
        namespaces "" '*'

    log "Creating VW kubeconfig for resource-broker"
    local cluster_id
    cluster_id="$(kubectl --kubeconfig "$ws_certmanager" \
        get apiexportendpointslices/certificates \
        -o jsonpath='{.metadata.annotations.kcp\.io/cluster}')"
    local endpoint_url="https://broker-platform-control-plane:32443/services/apiexport/$cluster_id/certificates/clusters/$cluster_id"
    local sa_token
    sa_token="$(kcp::serviceaccount::admin "$ws_certmanager" broker default)"
    kubeconfig::create::token "$vw_certmanager" "$endpoint_url" "$sa_token"
    kubectl::kubeconfig::secret "$ws_certmanager" "$vw_certmanager" \
        cert-manager default broker-platform-control-plane:32443

    log "Setting up consumer kcp workspace"
    kcp::create_workspace "$kubeconfigs/kcp-admin.kubeconfig" "$ws_consumer" "consumer"
}

_start_broker() {
    log "Starting broker"

    if [[ -z "$CI" ]]; then
        make docker-build-kcp || die "Failed to build resource-broker-kcp docker image"
    fi
    make kind-load-kcp KIND_CLUSTER=broker-platform \
        || die "Failed to load resource-broker-kcp image into kind cluster"

    KUBECONFIG="$kind_platform" \
        kubectl get secret operator-kubeconfig -o jsonpath='{.data.kubeconfig}' \
            | base64 -d \
            > "$kubeconfigs/operator.kubeconfig" \
            || die "Failed to get operator kubeconfig from kind cluster"
    yq -i '(.clusters[] | select(.name=="default") | .cluster.server) += ":platform"' \
        "$kubeconfigs/operator.kubeconfig" \
        || die "Failed to modify operator kubeconfig server URL"

    kubectl create secret generic kcp-kubeconfig \
        --namespace=resource-broker-system --dry-run=client -o yaml \
        --from-file=kubeconfig="$kubeconfigs/operator.kubeconfig" \
        | kubectl::apply "$kind_platform" "-"

    sed 's|Certificate.v1alpha1.example.platform-mesh.io|Certificate.v1alpha1.identity.generic.platform-mesh.io|' \
        examples/kcp-certs/platform/broker.yaml \
        | kubectl --kubeconfig "$kind_platform" apply -f-

    kubectl::wait "$kind_platform" broker/resource-broker resource-broker-system condition=Available
}

_cleanup() {
    log "Removing consumer Certificate"
    kubectl --kubeconfig "$ws_consumer" delete --ignore-not-found \
        -n default certificates.identity.generic.platform-mesh.io/cert-from-consumer

    log "Removing consumer APIBinding"
    kubectl --kubeconfig "$ws_consumer" delete --ignore-not-found \
        apibinding/certificates
}

case "$1" in
    (setup-platform) _setup_platform ;;
    (setup-provider) _setup_provider ;;
    (start-broker) _start_broker ;;
    (cleanup) _cleanup ;;
    (*) die "Unknown command: $1" ;;
esac
