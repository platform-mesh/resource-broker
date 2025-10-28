#!/usr/bin/env bash

log() { echo ">>> $@"; }
die() { echo "!!! $@" >&2; exit 1; }

# cd into repo root
cd "$(dirname "$0")/.." || die "Failed to change directory"

# set -x

command -v kind &>/dev/null || die "kind is not installed. Please install kind to proceed."

# kind get clusters | grep broker- | xargs -r -n1 kind delete cluster --name

tmpdir="$TMPDIR/example-broker"
# rm -rf "$tmpdir"
log "Using temporary directory: $tmpdir"

providers="$tmpdir/providers"
mkdir -p "$providers"

consumers="$tmpdir/consumers"
mkdir -p "$consumers"

_kind_cluster() {
    rm -f "$2"
    if ! kind get clusters | grep -q "^$1$"; then
        kind create cluster --name "$1" --kubeconfig "$2" || die "Failed to create cluster $1"
    else
        kind export kubeconfig --name "$1" --kubeconfig "$2" || die "Failed to export kubeconfig for cluster $1"
    fi
}

_kapply() {
    kubectl --kubeconfig "$1" apply -f "$2" || die "Failed to apply $2 to cluster with kubeconfig $1"
}

# log "Setting up platform cluster"
_kind_cluster broker-platform "$tmpdir/platform.kubeconfig"
# TODO: The platform cluster doesn't do anything _yet_. But it will be
# used to run store broker information and run workloads for migrations
# when that is implemented.

log "Setting up provider theseus"
_kind_cluster broker-theseus "$providers/theseus.kubeconfig"
_kapply "$providers/theseus.kubeconfig" ./config/crd/bases/broker.platform-mesh.io_acceptapis.yaml
_kapply "$providers/theseus.kubeconfig" ./config/crd/bases/example.platform-mesh.io_vms.yaml
_kapply "$providers/theseus.kubeconfig" ./example/theseus.yaml

log "Setting up provider nestor"
_kind_cluster broker-nestor "$providers/nestor.kubeconfig" || die "Failed to create provider nestor"
_kapply "$providers/nestor.kubeconfig" ./config/crd/bases/broker.platform-mesh.io_acceptapis.yaml
_kapply "$providers/nestor.kubeconfig" ./config/crd/bases/example.platform-mesh.io_vms.yaml
_kapply "$providers/nestor.kubeconfig" ./example/nestor.yaml
# TODO: instead of applying the VM CRD configure AcceptAPI with
# a template to something else (e.g. just plain configmap or secret,
# whatever works and isn't too complex to setup)

log "Setting up consumer homer"
_kind_cluster broker-homer "$consumers/homer.kubeconfig" || die "Failed to create consumer homer"
_kapply "$consumers/homer.kubeconfig" ./config/crd/bases/example.platform-mesh.io_vms.yaml

log "Starting broker"
# go run isn't being killed properly, instead the binary is built and
# run
make build
./bin/manager \
    -kubeconfig "$tmpdir/platform.kubeconfig" \
    -source-kubeconfig "$consumers" \
    -target-kubeconfig "$providers" \
    -group example.platform-mesh.io \
    -kind VM \
    -version v1alpha1 \
    &
broker_pid=$!
trap "kill $broker_pid; wait $broker_pid" EXIT

log "Deploying example VM in homer, should land in theseus"
_kapply "$consumers/homer.kubeconfig" ./example/homer.yaml

log "Waiting for VM to appear in theseus"
kubectl --kubeconfig "$providers/theseus.kubeconfig" wait --for=create vm/vm-from-homer || die "VM did not become ready in theseus"

log "Verifying VM is not in nestor"
if kubectl --kubeconfig "$providers/nestor.kubeconfig" get vm/vm-from-homer &>/dev/null; then
    die "VM should not be present in nestor"
fi

log "Change VM in consumer homer to land on nestor"
kubectl --kubeconfig "$consumers/homer.kubeconfig" patch vm vm-from-homer --type merge -p '{"spec":{"memory":5120}}' || die "Failed to patch VM in homer"

# TODO The VM should be first created in nestor and when it is ready
# deleted from thesues. But as the initial poc deleting and then
# creating is fine.

log "Wait for VM to vanish from theseus"
kubectl --kubeconfig "$providers/theseus.kubeconfig" wait --for=delete vm/vm-from-homer || die "VM did not get deleted from theseus"

log "Wait for VM to appear in nestor"
kubectl --kubeconfig "$providers/nestor.kubeconfig" wait --for=create vm/vm-from-homer || die "VM did not become ready in nestor"

log "Delete VM in consumer homer"
kubectl --kubeconfig "$consumers/homer.kubeconfig" delete vm/vm-from-homer || die "Failed to delete VM in homer"

log "Wait for VM to vanish from nestor"
kubectl --kubeconfig "$providers/nestor.kubeconfig" wait --for=delete vm/vm-from-homer || die "VM did not get deleted from nestor"
