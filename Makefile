# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.28.0

DOCKER_BUILD_PLATFORM = "--platform=linux/amd64"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Limit the scope of generation otherwise it will try to generate configs for non-controller code
GENPATH = "./api/v1;./internal/controller/..."

CHART_PATH            		= helm
CHART_OPERATOR_PATH   		= $(CHART_PATH)/soperator
CHART_OPERATOR_CRDS_PATH   	= $(CHART_PATH)/soperator-crds
CHART_CLUSTER_PATH    		= $(CHART_PATH)/slurm-cluster
CHART_STORAGE_PATH    		= $(CHART_PATH)/slurm-cluster-storage

SLURM_VERSION		  		= 24.05.2
UBUNTU_VERSION		  		= jammy
VERSION               		= $(shell cat VERSION)

IMAGE_VERSION		  = $(VERSION)-$(UBUNTU_VERSION)-slurm$(SLURM_VERSION)
GO_CONST_VERSION_FILE = internal/consts/version.go
GITHUB_REPO			  = ghcr.io/nebius/soperator
NEBIUS_REPO			  = cr.eu-north1.nebius.cloud/soperator
IMAGE_REPO			  = $(NEBIUS_REPO)

# For version sync test
VALUES_VERSION 		  = $(shell $(YQ) '.images.slurmctld' helm/slurm-cluster/values.yaml | awk -F':' '{print $$2}' | awk -F'-' '{print $$1}')


OPERATOR_IMAGE_TAG  = $(VERSION)

ifeq ($(shell uname), Darwin)
    SHA_CMD = shasum -a 256
    SED_COMMAND = sed -i '' -e
    USER_MAIL					= $(shell git config user.email)
else
    SHA_CMD = sha256sum
    SED_COMMAND = sed -i -e
    USER_MAIL					= $(shell git log -1 --pretty=format:'%ae')
endif
ifeq ($(UNSTABLE), true)
    SHORT_SHA 					= $(shell echo -n "$(USER_MAIL)-$(VERSION)" | $(SHA_CMD) | cut -c1-8)
    OPERATOR_IMAGE_TAG  		= $(VERSION)-$(SHORT_SHA)
    IMAGE_VERSION		  		= $(VERSION)-$(UBUNTU_VERSION)-slurm$(SLURM_VERSION)-$(SHORT_SHA)
    IMAGE_REPO			  		= $(NEBIUS_REPO)-unstable
endif

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
	go test ./... -coverprofile cover.out

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter & yamllint
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

.PHONY: helm
helm: kustomize helmify yq ## Update soperator Helm chart
	rm -rf $(CHART_OPERATOR_PATH)
	$(KUSTOMIZE) build config/default | $(HELMIFY) --crd-dir $(CHART_OPERATOR_PATH)
	rm -f $(CHART_PATH)/operatorAppVersion
	cp -r $(CHART_OPERATOR_PATH)/crds/* $(CHART_OPERATOR_CRDS_PATH)/templates/
	@$(YQ) -i ".name = \"helm-soperator\"" "$(CHART_OPERATOR_PATH)/Chart.yaml"
	@$(SED_COMMAND) '/^#/d' "$(CHART_OPERATOR_PATH)/Chart.yaml"

.PHONY: get-version
get-version:
ifeq ($(UNSTABLE), true)
	@echo '$(VERSION)-$(SHORT_SHA)'
else
	@echo '$(VERSION)'
endif

.PHONY: test-version-sync
test-version-sync: yq
	@if [ "$(VERSION)" != "$(VALUES_VERSION)" ]; then \
		echo "Version in version file and helm/slurm-cluster different!"; \
		echo "VERSION is - $(VERSION)"; \
		echo "VALUES_VERSION is - $(VALUES_VERSION)"; \
		exit 1; \
	else \
		echo "Version test passed: versions is: $(VERSION)"; \
	fi

.PHONY: get-operator-tag-version
get-operator-tag-version:
	@echo '$(OPERATOR_IMAGE_TAG)'

.PHONY: get-image-version
get-image-version:
	@echo '$(IMAGE_VERSION)'

.PHONY: sync-version
sync-version: yq ## Sync versions from file
	@echo 'Version is - $(VERSION)'
	@echo 'Image version is - $(IMAGE_VERSION)'
	@echo 'Operator image tag is - $(OPERATOR_IMAGE_TAG)'
	@# region config/manager/kustomization.yaml
	@echo 'Syncing config/manager/kustomization.yaml'
	@$(YQ) -i ".images.[0].newName = \"$(IMAGE_REPO)/slurm-operator\"" "config/manager/kustomization.yaml"
	@$(YQ) -i ".images.[0].newTag = \"$(OPERATOR_IMAGE_TAG)\"" "config/manager/kustomization.yaml"
	@# endregion config/manager/kustomization.yaml

	@# region config/manager/manager.yaml
	@echo 'Syncing config/manager/manager.yaml'
	@$(SED_COMMAND) "s/image: controller:[^ ]*/image: controller:$(OPERATOR_IMAGE_TAG)/" config/manager/manager.yaml
	@# endregion config/manager/manager.yaml

	@# region helm chart versions
	@echo 'Syncing helm chart versions'
	@$(YQ) -i ".version = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_OPERATOR_PATH)/Chart.yaml"
	@$(YQ) -i ".version = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_OPERATOR_CRDS_PATH)/Chart.yaml"
	@$(YQ) -i ".version = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_CLUSTER_PATH)/Chart.yaml"
	@$(YQ) -i ".version = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_STORAGE_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_OPERATOR_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_OPERATOR_CRDS_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_CLUSTER_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_STORAGE_PATH)/Chart.yaml"
	@# endregion helm chart versions
#
	@# region helm/slurm-cluster/values.yaml
	@echo 'Syncing helm/slurm-cluster/values.yaml'
	@$(YQ) -i ".images.ncclBenchmark = \"$(IMAGE_REPO)/nccl_benchmark:$(IMAGE_VERSION)\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.slurmctld = \"$(IMAGE_REPO)/controller_slurmctld:$(IMAGE_VERSION)\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.slurmdbd = \"$(IMAGE_REPO)/controller_slurmdbd:$(IMAGE_VERSION)\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.slurmd = \"$(IMAGE_REPO)/worker_slurmd:$(IMAGE_VERSION)\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.sshd = \"$(IMAGE_REPO)/login_sshd:$(IMAGE_VERSION)\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.munge = \"$(IMAGE_REPO)/munge:$(IMAGE_VERSION)\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.populateJail = \"$(IMAGE_REPO)/populate_jail:$(IMAGE_VERSION)\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.exporter = \"$(IMAGE_REPO)/exporter:$(IMAGE_VERSION)\"" "helm/slurm-cluster/values.yaml"
	@# endregion helm/slurm-cluster/values.yaml

	@# region helm/slurm-cluster/templates/_registry_helpers.tpl
	@echo "Syncing $(CHART_CLUSTER_PATH)/templates/_registry_helpers.tpl"
	@echo '{{/* This file is generated by make sync-version. */}}' >  $(CHART_CLUSTER_PATH)/templates/_registry_helpers.tpl
	@echo ''                                                       >>  $(CHART_CLUSTER_PATH)/templates/_registry_helpers.tpl
	@echo '{{/* Container registry with stable Docker images */}}' >> $(CHART_CLUSTER_PATH)/templates/_registry_helpers.tpl
	@echo '{{- define "slurm-cluster.containerRegistry" -}}'       >> $(CHART_CLUSTER_PATH)/templates/_registry_helpers.tpl
	@echo "    {{- \"$(IMAGE_REPO)\" -}}"           >> $(CHART_CLUSTER_PATH)/templates/_registry_helpers.tpl
	@echo "{{- end }}"                                             >> $(CHART_CLUSTER_PATH)/templates/_registry_helpers.tpl
	@# endregion helm/slurm-cluster/templates/_registry_helpers.tpl

	@# region helm/soperator/values.yaml
	@echo 'Syncing helm/soperator/values.yaml'
	@$(YQ) -i ".controllerManager.manager.image.repository = \"$(IMAGE_REPO)/slurm-operator\"" "helm/soperator/values.yaml"
	@$(YQ) -i ".controllerManager.manager.image.tag = \"$(OPERATOR_IMAGE_TAG)\"" "helm/soperator/values.yaml"
	@# endregion helm/soperator/values.yaml

	@# region internal/consts
	@echo "Syncing $(GO_CONST_VERSION_FILE)"
	@echo '// This file is generated by make sync-version.' >  $(GO_CONST_VERSION_FILE)
	@echo 'package consts'                                  >> $(GO_CONST_VERSION_FILE)
	@echo ''                                                >> $(GO_CONST_VERSION_FILE)
	@echo 'const ('                                         >> $(GO_CONST_VERSION_FILE)
	@echo "	VersionCR = \"$(OPERATOR_IMAGE_TAG)\""          >> $(GO_CONST_VERSION_FILE)
	@echo ')'                                               >> $(GO_CONST_VERSION_FILE)
	@# endregion internal/consts

.PHONY: sync-version-from-scratch
sync-version-from-scratch: generate manifests helm sync-version ## Regenerates all resources and syncs versions to them

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary with native toolchain.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host with native toolchain.
	go run ./cmd/main.go

.PHONY: docker-build
docker-build: ## Build docker image
ifndef IMAGE_NAME
	$(error IMAGE_NAME is not set, docker image cannot be built)
endif
ifndef DOCKERFILE
	$(error DOCKERFILE is not set, docker image cannot be built)
endif
ifeq (${IMAGE_NAME},slurm-operator)
	docker build $(DOCKER_BUILD_ARGS) --tag $(IMAGE_REPO)/${IMAGE_NAME}:${IMAGE_VERSION} --target ${IMAGE_NAME} ${DOCKER_IGNORE_CACHE} ${DOCKER_LOAD} ${DOCKER_BUILD_PLATFORM} -f ${DOCKERFILE} ${DOCKER_OUTPUT} .
else
	cd images && docker build $(DOCKER_BUILD_ARGS) --tag $(IMAGE_REPO)/${IMAGE_NAME}:${IMAGE_VERSION} --target ${IMAGE_NAME} ${DOCKER_IGNORE_CACHE} ${DOCKER_LOAD} ${DOCKER_BUILD_PLATFORM} -f ${DOCKERFILE} ${DOCKER_OUTPUT} .
endif

.PHONY: docker-push
docker-push: ## Push docker image
ifndef IMAGE_NAME
	$(error IMAGE_NAME is not set, docker image can not be pushed)
endif
	docker push "$(IMAGE_REPO)/${IMAGE_NAME}:${IMAGE_VERSION}"
ifeq ($(UNSTABLE), false)
	docker tag "$(IMAGE_REPO)/${IMAGE_NAME}:${IMAGE_VERSION}" "$(GITHUB_REPO)/${IMAGE_NAME}:${IMAGE_VERSION}"
	docker push "$(GITHUB_REPO)/${IMAGE_NAME}:${IMAGE_VERSION}"
endif

.PHONY: release-helm
release-helm: ## Build & push helm docker image
	mkdir -p "helm-releases"
	@echo "helm release for unstable version"
	./release_helm.sh  -v "${OPERATOR_IMAGE_TAG}" -u "$(IMAGE_REPO)"
ifeq ($(UNSTABLE), false)
	@echo "helm release for stable version"
	./release_helm.sh -v "${OPERATOR_IMAGE_TAG}" -u "$(NEBIUS_REPO)"
endif
	rm -rf /helm-releases/*

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
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMAGE_REPO)/slurm-operator:$(OPERATOR_IMAGE_TAG)
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
KUSTOMIZE_VERSION        ?= v5.5.0
CONTROLLER_TOOLS_VERSION ?= v0.14.0
ENVTEST_VERSION          ?= release-0.17
GOLANGCI_LINT_VERSION    ?= v1.57.2
HELMIFY_VERSION          ?= 0.4.13
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
