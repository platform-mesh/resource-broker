#!/usr/bin/env bash

# cd into repo root
example_dir="$(dirname "$0")"
cd "$(dirname "$0")/../.."
source "./hack/lib.bash"

# TODO scrub the ones that are not relevant
kubeconfigs="$PWD/kubeconfigs"
workspace_kubeconfigs="$kubeconfigs/workspaces"
mkdir -p "$workspace_kubeconfigs"

kind_platform="$kubeconfigs/platform.kubeconfig"
ws_platform="$workspace_kubeconfigs/platform.kubeconfig"

kind_internalca="$kubeconfigs/internalca.kubeconfig"
ws_internalca="$workspace_kubeconfigs/internalca.kubeconfig"

kind_externalca="$kubeconfigs/externalca.kubeconfig"
ws_externalca="$workspace_kubeconfigs/externalca.kubeconfig"

kind_consumer="$kubeconfigs/consumer.kubeconfig"
ws_consumer="$workspace_kubeconfigs/consumer.kubeconfig"
vw_consumer="$workspace_kubeconfigs/consumer.vw.kubeconfig"
# END TODO

_cluster_id() {
    local kubeconfig="$1"
    local resource="$2"
    kubectl --kubeconfig "$kubeconfig" get "$resource" \
        -o jsonpath='{.metadata.annotations.kcp\.io/cluster}'
}

__setup_provider() {
    local name="$1"
    local kind_kubeconfig="$2"  # This is the platform-mesh cluster kubeconfig
    local ws_kubeconfig="$3"
    local suffix="$4"

    log "Creating APIExport certificates in $name workspace"
    {
        echo "apiVersion: apis.kcp.io/v1alpha1"
        echo "kind: APIExport"
        echo "metadata:"
        echo "  name: certificates"
    } | kubectl::apply "$ws_kubeconfig" "-"

    log "Setting up $name provider in platform-mesh cluster"
    kubectl create namespace "$name" --dry-run=client -o yaml \
        | kubectl::apply "$kind_kubeconfig" -

    kubectl create namespace default --dry-run=client -o yaml \
        | kubectl::apply "$ws_kubeconfig" -
    kubectl::kustomize "$ws_kubeconfig" "./examples/platform-mesh/root:providers:$name"

    kubectl::kustomize "$kind_kubeconfig" "./examples/platform-mesh/$name"

    log "Setting up api-syncagent for $name in platform-mesh cluster"

    local api_syncagent_token="$(kcp::serviceaccount::admin "$ws_kubeconfig" api-syncagent default)"
    local api_syncagent_kubeconfig="$kubeconfigs/api-syncagent-$name.kubeconfig"

    # Create kubeconfig with in-cluster service URL
    # Get the current server URL and extract the workspace path
    local current_url="$(kubectl::kubeconfig::current_server_url "$ws_kubeconfig")"
    local ws_path="${current_url#*:8443}"

    kubeconfig::create::token "$api_syncagent_kubeconfig" \
        "https://localhost:8443${ws_path}" \
        "$api_syncagent_token"

    kubectl::kubeconfig::secret "$kind_kubeconfig" "$api_syncagent_kubeconfig" "$name" "$name" ""

    helm::install::api_syncagent "$kind_kubeconfig" "certificates" "$name" "kubeconfig-$name" \
        --skip-crds \
        --namespace "$name" \
        --set "crds.enabled=false" \
        --set "hostAliases.enabled=true" \
        --set "hostAliases.values[0].ip=10.96.188.4" \
        --set "hostAliases.values[0].hostnames[0]=localhost" \
        --set "publishedResourceSelector=ca in ($name)"

    PROJECTION_GROUP="pki.generic.platform-mesh.io" LABEL_KEY=ca LABEL_VALUE="$name" NAMESPACE="$name" \
        AGENT_NAME="$name" \
        apisyncagent::publish "$kind_kubeconfig" \
        "certificates" "Certificate" "$name.ca" "v1alpha1" \
        "certificate" "service" "Secret" "status.relatedResources.secret.name"

    log "Bind APIExport $name locally in $name workspace"
    kcp::apibinding "$ws_kubeconfig" "root:providers:$name" certificates \
        secrets "" '*' \
        events "" '*' \
        namespaces "" '*'

    # Grab the VW endpoint URL for later use (with in-cluster service)
    local cluster_id="$(_cluster_id "$ws_kubeconfig" apiexportendpointslices/certificates)"
    local endpoint_url="https://localhost:8443/services/apiexport/$cluster_id/certificates/clusters/$cluster_id"

    kubectl create namespace default --dry-run=client -o yaml \
        | kubectl::apply "$ws_kubeconfig" -

    # Create a service account for the broker to use; this should get proper
    # RBAC in a prod setup
    local sa_token="$(kcp::serviceaccount::admin "$ws_kubeconfig" broker default)"
    # Create a kubeconfig for the VW
    local ws_vw="${ws_kubeconfig%%.kubeconfig}.vw.kubeconfig"
    kubeconfig::create::token "$ws_vw" "$endpoint_url" "$sa_token"

    # Create a secret with the kubeconfig, this will be pulled by the
    # broker.
    kubectl::kubeconfig::secret "$ws_kubeconfig" "$ws_vw" "$name" default ""
}

_setup_provider() {
    local name="$1"
    local suffix="$2"
    local pm_kubeconfig="$3"
    local compute_kubeconfig="$4"  # Add compute kubeconfig parameter
    local ws_kubeconfig="$name.pm.kubeconfig"

    # install api-syncagent CRDs separately so helm doesn't throw a fit
    # when installing the helm chart twice
    KUBECONFIG="$compute_kubeconfig" kubectl apply -f 'https://raw.githubusercontent.com/kcp-dev/api-syncagent/refs/heads/main/deploy/crd/kcp.io/syncagent.kcp.io_publishedresources.yaml' \
        || die "error installing api-syncagent CRD"

    cp "$pm_kubeconfig" "$ws_kubeconfig"
    KUBECONFIG="$ws_kubeconfig" kubectl ws :root:providers
    KUBECONFIG="$ws_kubeconfig" kubectl create-workspace \
        --enter --ignore-existing \
        --type root:provider \
        "$name"
    __setup_provider "$name" "$compute_kubeconfig" "$ws_kubeconfig" "$suffix"
    kubectl::kustomize "$ws_kubeconfig" "./examples/platform-mesh/root:providers:$name"
}

_start_broker() {
    local kind_kubeconfig="$1"
    local pm_kubeconfig="$2"

    # docker images are already loaded into the pm kind cluster
    local op_kubeconfig="rb.kubeconfig"
    cp "$pm_kubeconfig" "$op_kubeconfig"
    KUBECONFIG="$op_kubeconfig" kubectl ws :root:providers:resource-broker

    # For in-cluster use, replace localhost with the in-cluster service name
    yq -i '(.clusters[] | .cluster.server) = "https://localhost:8443/clusters/root:providers:resource-broker"' \
        "$op_kubeconfig" \
        || die "Failed to modify operator kubeconfig server URL"

    kubectl create namespace resource-broker-system --dry-run=client -o yaml \
        | kubectl::apply "$kind_kubeconfig" -

    kubectl create secret generic kcp-kubeconfig \
        --namespace=resource-broker-system \
        --dry-run=client -o yaml \
        --from-file=kubeconfig="$op_kubeconfig" \
        | kubectl::apply "$kind_kubeconfig" "-"

    kubectl::apply "$kind_kubeconfig" ./examples/platform-mesh/resource-broker/broker.yaml

    kubectl::wait "$kind_kubeconfig" broker/resource-broker resource-broker-system condition=Available
}

cmd="$1"
shift

case "$cmd" in
    (setup-provider) _setup_provider "$@";;
    (start-broker) _start_broker "$@";;
    (*) die "Unknown command: $1" ;;
esac
