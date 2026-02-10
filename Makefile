# Image URL to use all building/pushing image targets
IMG ?= resource-broker:dev
IMG_KCP ?= resource-broker-kcp:dev
IMG_OPERATOR ?= resource-broker-operator:dev

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

MODULES = $(shell find . -iname 'go.mod' -exec dirname {} \;)

.PHONY: all
all: build

.PHONY: check
check: codegen lint-fix test test-e2e ## Run linters and tests.
	@echo "All checks passed."

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) crd rbac:roleName=resource-broker output:crd:dir=config/broker/crd output:rbac:dir=config/broker/rbac paths="./api/broker/..."
	$(CONTROLLER_GEN) crd rbac:roleName=resource-broker-example output:crd:dir=config/example/crd output:rbac:dir=config/example/rbac paths="./api/example/..."
	$(CONTROLLER_GEN) crd rbac:roleName=resource-broker-operator output:crd:dir=config/operator/crd output:rbac:dir=config/operator/rbac paths="./api/operator/..."

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object paths="./api/..."

.PHONY: codegen
codegen: manifests generate ## Generate code and manifests.

.PHONY: fmt
fmt: ## Run go fmt against code.
	@for module in $(MODULES); do \
		echo "Running go fmt in $$module"; \
		cd $$module && go fmt ./... || exit 1; \
		cd - > /dev/null; \
	done

.PHONY: vet
vet: ## Run go vet against code.
	@for module in $(MODULES); do \
		echo "Running go vet in $$module"; \
		cd $$module && go vet ./... || exit 1; \
		cd - > /dev/null; \
	done

.PHONY: test
test: manifests generate fmt vet ## Run tests.
	go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out $(TEST_ARGS)

.PHONY: test-e2e
test-e2e: manifests generate fmt vet setup-envtest ## Run the e2e tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
		go test ./test/e2e/... -coverprofile cover-e2e.out $(TEST_ARGS)

EXAMPLES ?= $(patsubst examples/%,%,$(wildcard examples/*))
EXAMPLES_PREFIX ?= $(patsubst %,example-%,$(EXAMPLES))

.PHONY: examples
examples: $(EXAMPLES_PREFIX) ## Run the examples.
example-%: manifests generate fmt vet
	go run github.com/ntnn/mdextract/cmd/mdextract@main -output run.bash -tags bash,ci ./examples/$*/README.md \
		&& trap 'rm -f run.bash' EXIT \
		&& bash -xe ./run.bash

GOLANGCI_LINT_CONFIG ?= $(PWD)/.golangci.yml

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	@for module in $(MODULES); do \
		echo "Running golangci-lint in $$module"; \
		cd $$module && $(GOLANGCI_LINT) run --config $(GOLANGCI_LINT_CONFIG) || exit 1; \
		cd - > /dev/null; \
	done

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	@for module in $(MODULES); do \
		echo "Running golangci-lint in $$module"; \
		cd $$module && $(GOLANGCI_LINT) run --config $(GOLANGCI_LINT_CONFIG) --fix || exit 1; \
		cd - > /dev/null; \
	done

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	$(GOLANGCI_LINT) config verify

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager ./cmd/

.PHONY: build-operator
build-operator: manifests generate fmt vet ## Build operator binary.
	go build -o bin/operator ./cmd/operator

.PHONY: run
run: manifests generate fmt vet ## Run manager from your host.
	go run ./cmd/ -zap-devel=true -zap-log-level=debug

.PHONY: run-operator
run-operator: manifests generate fmt vet ## Run operator from your host.
	go run ./cmd/operator -zap-devel=true -zap-log-level=debug

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} .

.PHONY: docker-build-kcp
docker-build-kcp: ## Build docker image with the operator for kcp.
	$(CONTAINER_TOOL) build -t ${IMG_KCP} -f contrib/kcp/Dockerfile .

.PHONY: docker-build-operator
docker-build-operator: ## Build docker image with the operator.
	$(CONTAINER_TOOL) build -t ${IMG_OPERATOR} -f cmd/operator/Dockerfile .

KIND_CLUSTER ?= kind

.PHONY: kind-load
kind-load: ## Load docker image with the broker into kind cluster. Set cluster name with KIND_CLUSTER.
	kind load docker-image --name "$(KIND_CLUSTER)" "${IMG}"

.PHONY: kind-load-kcp
kind-load-kcp: ## Load docker image with the operator for kcp into kind cluster. Set cluster name with KIND_CLUSTER.
	kind load docker-image --name "$(KIND_CLUSTER)" "${IMG_KCP}"

.PHONY: kind-load-operator
kind-load-operator: ## Load docker image with the operator into kind cluster. Set cluster name with KIND_CLUSTER.
	kind load docker-image --name "$(KIND_CLUSTER)" "${IMG_OPERATOR}"

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install broker CRDs.
	$(KUSTOMIZE) build config/broker/crd | $(KUBECTL) apply -f -

.PHONY: install-example
install-example: manifests kustomize ## Install example CRDs.
	$(KUSTOMIZE) build config/example/crd | $(KUBECTL) apply -f -

.PHONY: install-operator
install-operator: manifests kustomize ## Install operator CRDs.
	$(KUSTOMIZE) build config/operator/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall broker CRDs.
	$(KUSTOMIZE) build config/broker/crd | $(KUBECTL) delete --ignore-not-found=true -f -

.PHONY: uninstall-example
uninstall-example: manifests kustomize ## Uninstall example CRDs.
	$(KUSTOMIZE) build config/example/crd | $(KUBECTL) delete --ignore-not-found=true -f -

.PHONY: uninstall-operator
uninstall-operator: manifests kustomize ## Uninstall operator CRDs.
	$(KUSTOMIZE) build config/operator/crd | $(KUBECTL) delete --ignore-not-found=true -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy broker.
	cd config/broker/default && $(KUSTOMIZE) edit set image broker=${IMG}
	$(KUBECTL) create namespace --dry-run=client resource-broker-system -o yaml | $(KUBECTL) apply -f -
	$(KUSTOMIZE) build config/broker/default | $(KUBECTL) apply -f -

.PHONY: deploy-example
deploy-example: manifests kustomize ## Deploy example.
	$(KUSTOMIZE) build config/example/default | $(KUBECTL) apply -f -

.PHONY: deploy-operator
deploy-operator: manifests kustomize ## Deploy operator.
	cd config/operator/default && $(KUSTOMIZE) edit set image operator=${IMG_OPERATOR}
	$(KUBECTL) create namespace --dry-run=client resource-broker-system -o yaml | $(KUBECTL) apply -f -
	$(KUSTOMIZE) build config/operator/default | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy broker.
	$(KUSTOMIZE) build config/broker/default | $(KUBECTL) delete --ignore-not-found=true -f -

.PHONY: undeploy-example
undeploy-example: kustomize ## Undeploy example.
	$(KUSTOMIZE) build config/example/default | $(KUBECTL) delete --ignore-not-found=true -f -

.PHONY: undeploy-operator
undeploy-operator: kustomize ## Undeploy operator.
	$(KUSTOMIZE) build config/operator/default | $(KUBECTL) delete --ignore-not-found=true -f -

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v5.5.0
CONTROLLER_TOOLS_VERSION ?= v0.17.2
#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell go list -m -f "{{ .Version }}" sigs.k8s.io/controller-runtime | awk -F'[v.]' '{printf "release-%d.%d", $$2, $$3}')
#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')
GOLANGCI_LINT_VERSION ?= v2.4.0

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef
