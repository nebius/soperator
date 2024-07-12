# Stable images get pulled from here
# https://console.nebius.ai/folders/bje82q7sm8njm3c4rrlq/container-registry/registries/crnlu9nio77sg3p8n5bi/overview
CONTAINER_REGISTRY_STABLE_ID = crnlu9nio77sg3p8n5bi
CONTAINER_REGISTRY_STABLE = cr.nemax.nebius.cloud/$(CONTAINER_REGISTRY_STABLE_ID)

# OCI for Helm charts
# https://console.nebius.ai/folders/bje82q7sm8njm3c4rrlq/container-registry/registries/crnefnj17i4kqgt3up94/overview
CONTAINER_REGISTRY_HELM_ID = crnefnj17i4kqgt3up94

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.28.0

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
CONTAINER_TOOL ?= bazel

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# If this variable is set to use bazel's driver it will lead to panics in gen-controller
unexport GOPACKAGESDRIVER
# Limit the scope of generation otherwise it will try to generate configs for non-controller code
GENPATH = "./api/v1;./internal/controller/..."

CHART_PATH            = helm
CHART_OPERATOR_PATH   = $(CHART_PATH)/slurm-operator
CHART_CLUSTER_PATH    = $(CHART_PATH)/slurm-cluster
CHART_STORAGE_PATH    = $(CHART_PATH)/slurm-cluster-storage

VERSION               = $(shell cat VERSION)
GO_CONST_VERSION_FILE = internal/consts/version.go

# Image URL to use all building/pushing image targets
OPERATOR_IMAGE_REPO ?= $(CONTAINER_REGISTRY_STABLE)/slurm-operator
OPERATOR_IMAGE_TAG  ?= $(VERSION)

.PHONY: all
all: build

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
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths=$(GENPATH) output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object paths=$(GENPATH)

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -coverprofile cover.out

.PHONY: bazel-test ## Run test with bazel. `vet` not working with bazel (locally)
bazel-test: manifests generate fmt
	bazel test "..."

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter & yamllint
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

.PHONY: gazelle
gazelle: ## Run gazelle
	bazel run //:gazelle -- msp/slurm-service/internal/operator

.PHONY: helm
helm: kustomize helmify yq ## Update slurm-operator Helm chart
	rm -rf $(CHART_OPERATOR_PATH)
	$(KUSTOMIZE) build config/default | $(HELMIFY) --crd-dir $(CHART_OPERATOR_PATH)
	rm -f $(CHART_PATH)/operatorAppVersion

	$(YQ) -i 'del(.controllerManager.manager.image.tag)' "$(CHART_OPERATOR_PATH)/values.yaml"

.PHONY: sync-version
sync-version: ## Sync versions from file
	@# region config/manager/kustomization.yaml
	@echo 'Syncing config/manager/kustomization.yaml'
	@$(YQ) -i ".images.[0].newName = \"$(CONTAINER_REGISTRY_STABLE)/slurm-operator\"" "config/manager/kustomization.yaml"
	@$(YQ) -i ".images.[0].newTag = \"$(VERSION)\"" "config/manager/kustomization.yaml"
	@# endregion config/manager/kustomization.yaml

	@# region config/manager/manager.yaml
	@echo 'Syncing config/manager/manager.yaml'
	@sed -i '' -e "s/image: controller:[^ ]*/image: controller:$(VERSION)/" config/manager/manager.yaml
	@# endregion config/manager/manager.yaml

	@# region helm chart versions
	@echo 'Syncing helm chart versions'
	@$(YQ) -i ".version = \"$(VERSION)\"" "$(CHART_OPERATOR_PATH)/Chart.yaml"
	@$(YQ) -i ".version = \"$(VERSION)\"" "$(CHART_CLUSTER_PATH)/Chart.yaml"
	@$(YQ) -i ".version = \"$(VERSION)\"" "$(CHART_STORAGE_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(VERSION)\"" "$(CHART_OPERATOR_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(VERSION)\"" "$(CHART_CLUSTER_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(VERSION)\"" "$(CHART_STORAGE_PATH)/Chart.yaml"
	@# endregion helm chart versions

	@# region helm/slurm-cluster/values.yaml
	@echo 'Syncing helm/slurm-cluster/values.yaml'
	@$(YQ) -i ".periodicChecks.ncclBenchmark.image = \"$(CONTAINER_REGISTRY_STABLE)/nccl_benchmark\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.slurmctld = \"$(CONTAINER_REGISTRY_STABLE)/controller_slurmctld\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.slurmd = \"$(CONTAINER_REGISTRY_STABLE)/worker_slurmd\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.sshd = \"$(CONTAINER_REGISTRY_STABLE)/login_sshd\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.munge = \"$(CONTAINER_REGISTRY_STABLE)/munge\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.populateJail = \"$(CONTAINER_REGISTRY_STABLE)/populate_jail\"" "helm/slurm-cluster/values.yaml"
	@# endregion helm/slurm-cluster/values.yaml

	@# region helm/slurm-cluster/templates/_registry_helpers.tpl
	@echo "Syncing $(CHART_CLUSTER_PATH)/templates/_registry_helpers.tpl"
	@echo '{{/* This file is generated by make sync-version. */}}' >  $(CHART_CLUSTER_PATH)/templates/_registry_helpers.tpl
	@echo ''                                                       >>  $(CHART_CLUSTER_PATH)/templates/_registry_helpers.tpl
	@echo '{{/* Container registry with stable Docker images */}}' >> $(CHART_CLUSTER_PATH)/templates/_registry_helpers.tpl
	@echo '{{- define "slurm-cluster.containerRegistry" -}}'       >> $(CHART_CLUSTER_PATH)/templates/_registry_helpers.tpl
	@echo "    {{- \"$(CONTAINER_REGISTRY_STABLE)\" -}}"           >> $(CHART_CLUSTER_PATH)/templates/_registry_helpers.tpl
	@echo "{{- end }}"                                             >> $(CHART_CLUSTER_PATH)/templates/_registry_helpers.tpl
	@# endregion helm/slurm-cluster/templates/_registry_helpers.tpl

	@# region helm/slurm-operator/values.yaml
	@echo 'Syncing helm/slurm-operator/values.yaml'
	@$(YQ) -i ".controllerManager.manager.image.repository = \"$(CONTAINER_REGISTRY_STABLE)/slurm-operator\"" "helm/slurm-operator/values.yaml"
	@$(YQ) -i ".controllerManager.manager.image.tag = \"$(VERSION)\"" "helm/slurm-operator/values.yaml"
	@# endregion helm/slurm-operator/values.yaml

	@# region internal/consts
	@echo "Syncing $(GO_CONST_VERSION_FILE)"
	@echo '// This file is generated by make sync-version.' >  $(GO_CONST_VERSION_FILE)
	@echo 'package consts'                                  >> $(GO_CONST_VERSION_FILE)
	@echo ''                                                >> $(GO_CONST_VERSION_FILE)
	@echo 'const ('                                         >> $(GO_CONST_VERSION_FILE)
	@echo "	VersionCR = \"$(VERSION)\""                     >> $(GO_CONST_VERSION_FILE)
	@echo ')'                                               >> $(GO_CONST_VERSION_FILE)
	@# endregion internal/consts

	@# region release_helm.sh
	@echo 'Syncing release_helm.sh'
	@sed -i '' -e "s/CONTAINER_REGISTRY_ID=[^ ]*/CONTAINER_REGISTRY_ID='$(CONTAINER_REGISTRY_HELM_ID)'/" release_helm.sh
	@# endregion release_helm.sh

	@# region terraform/terraform.tfvars.example
	@echo 'Syncing terraform/terraform.tfvars.example'
	@sed -i '' -e 's/slurm_operator_version = "[^ ]*/slurm_operator_version = "$(VERSION)"/' terraform/terraform.tfvars.example
	@# endregion terraform/terraform.tfvars.example

	@# region terraform/slurm_cluster_variables.tf
	@echo 'Syncing terraform/slurm_cluster_variables.tf'
	@sed -i '' -e 's/default *= *"0.1.[^ ]*/default = "$(VERSION)"/' terraform/slurm_cluster_variables.tf
	@terraform fmt terraform/slurm_cluster_variables.tf
	@# endregion terraform/slurm_cluster_variables.tf

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary with native toolchain.
	go build -o bin/manager cmd/main.go

.PHONY: bazel-build
bazel-build: manifests generate fmt  ## Build manager binary with bazel.
	bazel build "..."

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host with native toolchain.
	go run ./cmd/main.go

.PHONY: bazel-run
bazel-run: manifests generate fmt  ## Run a controller from your host with bazel.
	bazel run //msp/slurm-service/internal/operator/cmd:cmd

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) run //msp/slurm-service/internal/operator/docker:image -- --repository $(OPERATOR_IMAGE_REPO) --tag $(OPERATOR_IMAGE_TAG)

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) run //msp/slurm-service/internal/operator/docker:push_stable -- --repository $(OPERATOR_IMAGE_REPO) --tag $(OPERATOR_IMAGE_TAG)

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(OPERATOR_IMAGE_REPO):$(OPERATOR_IMAGE_TAG)
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL        ?= kubectl
KUSTOMIZE      ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST        ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT   = $(LOCALBIN)/golangci-lint
HELMIFY        ?= $(LOCALBIN)/helmify
YQ             ?= $(LOCALBIN)/yq

## Tool Versions
KUSTOMIZE_VERSION        ?= v5.3.0
CONTROLLER_TOOLS_VERSION ?= v0.14.0
ENVTEST_VERSION          ?= release-0.17
GOLANGCI_LINT_VERSION    ?= v1.57.2
HELMIFY_VERSION          ?= 0.4.11
YQ_VERSION               ?= 4.44.1

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || GOBIN=$(LOCALBIN) GO111MODULE=on go install sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	@[ -f $(GOLANGCI_LINT) ] || { \
	set -e ;\
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell dirname $(GOLANGCI_LINT)) $(GOLANGCI_LINT_VERSION) ;\
	}

.PHONY: helmify
helmify: $(HELMIFY) ## Download helmify locally if necessary.
$(HELMIFY): $(LOCALBIN)
	test -s $(LOCALBIN)/helmify || GOBIN=$(LOCALBIN) go install github.com/arttor/helmify/cmd/helmify@v$(HELMIFY_VERSION)

.PHONY: yq
yq: $(YQ) ## Download yq locally if necessary.
$(YQ): $(LOCALBIN)
	test -s $(LOCALBIN)/yq || GOBIN=$(LOCALBIN) go install github.com/mikefarah/yq/v4@v$(YQ_VERSION)
