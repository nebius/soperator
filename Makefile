# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.28.0

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
GENPATH = "./api/v1;./api/v1alpha1;"

CHART_PATH            		  = helm
CHART_OPERATOR_PATH   		  = $(CHART_PATH)/soperator
CHART_SOPERATORCHECKS_PATH    = $(CHART_PATH)/soperatorchecks
CHART_NODECONFIGURATOR_PATH   = $(CHART_PATH)/nodeconfigurator
CHART_OPERATOR_CRDS_PATH   	  = $(CHART_PATH)/soperator-crds
CHART_CLUSTER_PATH    		  = $(CHART_PATH)/slurm-cluster
CHART_STORAGE_PATH    		  = $(CHART_PATH)/slurm-cluster-storage
CHART_FLUXCD_PATH    		  = $(CHART_PATH)/soperator-fluxcd
CHART_ACTIVECHECK_PATH        = $(CHART_PATH)/soperator-activechecks
CHART_DCGM_EXPORTER_PATH      = $(CHART_PATH)/soperator-dcgm-exporter
CHART_SOPERATOR_NOTIFIER_PATH = $(CHART_PATH)/soperator-notifier

SLURM_VERSION		  		= 24.11.6
UBUNTU_VERSION		  		?= noble
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
    SED_COMMAND = sed -i '' -e
else
    SED_COMMAND = sed -i -e
endif

ifeq ($(UNSTABLE), true)
    SHORT_SHA 					= $(shell git rev-parse --short=8 HEAD)
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
	$(CONTROLLER_GEN) crd webhook paths=$(GENPATH) output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=nodeconfigurator-role paths="./internal/rebooter/..." output:artifacts:config=config/rbac/nodeconfigurator/
	$(CONTROLLER_GEN) rbac:roleName=manager-role paths="./internal/controller/clustercontroller/...;  ./internal/controller/topologyconfcontroller/...; ./internal/controller/nodeconfigurator/...; ./internal/controller/nodesetcontroller/..." output:artifacts:config=config/rbac/clustercontroller/
	$(CONTROLLER_GEN) rbac:roleName=soperator-checks-role paths="./internal/controller/soperatorchecks/..." output:artifacts:config=config/rbac/soperatorchecks/
.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object paths=$(GENPATH)

.PHONY: fmt
fmt: ## Run go fmt against code.
	GOEXPERIMENT=synctest go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	GOEXPERIMENT=synctest go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	GOEXPERIMENT=synctest go test ./... # TODO: remove "GOEXPERIMENT=synctest" from everywhere after upgrading to Go 1.25.

.PHONY: test-coverage
test-coverage: manifests generate fmt vet envtest ## Run tests with coverage.
	GOEXPERIMENT=synctest go test ./... -coverprofile cover.out

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter & yamllint
	GOEXPERIMENT=synctest $(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	GOEXPERIMENT=synctest $(GOLANGCI_LINT) run --fix

.PHONY: helm
helm: generate manifests kustomize helmify ## Update soperator Helm chart
	$(KUSTOMIZE) build config/crd/bases > $(CHART_OPERATOR_PATH)/crds/slurmcluster-crd.yaml
	$(KUSTOMIZE) build config/crd/bases > $(CHART_OPERATOR_CRDS_PATH)/templates/slurmcluster-crd.yaml
# Because of helmify rewrite a file we need to make backup of values.yaml
	mv $(CHART_OPERATOR_PATH)/values.yaml $(CHART_OPERATOR_PATH)/values.yaml.bak
	mv $(CHART_NODECONFIGURATOR_PATH)/values.yaml $(CHART_NODECONFIGURATOR_PATH)/values.yaml.bak
	mv $(CHART_SOPERATORCHECKS_PATH)/values.yaml $(CHART_SOPERATORCHECKS_PATH)/values.yaml.bak
	$(KUSTOMIZE)  build --load-restrictor LoadRestrictionsNone config/rbac/clustercontroller  | $(HELMIFY) $(CHART_OPERATOR_PATH)
	$(KUSTOMIZE)  build --load-restrictor LoadRestrictionsNone config/rbac/nodeconfigurator  | $(HELMIFY) $(CHART_NODECONFIGURATOR_PATH)
	$(KUSTOMIZE)  build --load-restrictor LoadRestrictionsNone config/rbac/soperatorchecks  | $(HELMIFY) $(CHART_SOPERATORCHECKS_PATH)
	mv $(CHART_OPERATOR_PATH)/values.yaml.bak $(CHART_OPERATOR_PATH)/values.yaml
	mv $(CHART_NODECONFIGURATOR_PATH)/values.yaml.bak $(CHART_NODECONFIGURATOR_PATH)/values.yaml
	mv $(CHART_SOPERATORCHECKS_PATH)/values.yaml.bak $(CHART_SOPERATORCHECKS_PATH)/values.yaml
# Because of helmify rewrite a file we need to add the missing if statement
	@$(SED_COMMAND) '1s|^|{{- if and .Values.rebooter.generateRBAC .Values.rebooter.enabled }}\n|' $(CHART_NODECONFIGURATOR_PATH)/templates/nodeconfigurator-rbac.yaml
	@echo -e "\n{{- end }}" >> $(CHART_NODECONFIGURATOR_PATH)/templates/nodeconfigurator-rbac.yaml

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

	@echo 'Syncing config/soperatorchecks/kustomization.yaml'
	@$(YQ) -i ".images.[0].newName = \"$(IMAGE_REPO)/soperatorchecks\"" "config/soperatorchecks/kustomization.yaml"
	@$(YQ) -i ".images.[0].newTag = \"$(OPERATOR_IMAGE_TAG)\"" "config/soperatorchecks/kustomization.yaml"
	@# endregion config/soperatorchecks/kustomization.yaml

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
	@$(YQ) -i ".version = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_SOPERATORCHECKS_PATH)/Chart.yaml"
	@$(YQ) -i ".version = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_NODECONFIGURATOR_PATH)/Chart.yaml"
	@$(YQ) -i ".version = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_FLUXCD_PATH)/Chart.yaml"
	@$(YQ) -i ".version = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_ACTIVECHECK_PATH)/Chart.yaml"
	@$(YQ) -i ".version = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_DCGM_EXPORTER_PATH)/Chart.yaml"
	@$(YQ) -i ".version = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_SOPERATOR_NOTIFIER_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_OPERATOR_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_OPERATOR_CRDS_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_CLUSTER_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_STORAGE_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_SOPERATORCHECKS_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_NODECONFIGURATOR_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_FLUXCD_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_ACTIVECHECK_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_DCGM_EXPORTER_PATH)/Chart.yaml"
	@$(YQ) -i ".appVersion = \"$(OPERATOR_IMAGE_TAG)\"" "$(CHART_SOPERATOR_NOTIFIER_PATH)/Chart.yaml"
	@# endregion helm chart versions
#
	@# region helm/slurm-cluster/values.yaml
	@echo 'Syncing helm/slurm-cluster/values.yaml'
	@$(YQ) -i ".images.slurmctld = \"$(IMAGE_REPO)/controller_slurmctld:$(IMAGE_VERSION)\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.slurmrestd = \"$(IMAGE_REPO)/slurmrestd:$(IMAGE_VERSION)\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.slurmdbd = \"$(IMAGE_REPO)/controller_slurmdbd:$(IMAGE_VERSION)\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.slurmd = \"$(IMAGE_REPO)/worker_slurmd:$(IMAGE_VERSION)\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.sshd = \"$(IMAGE_REPO)/login_sshd:$(IMAGE_VERSION)\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.munge = \"$(IMAGE_REPO)/munge:$(IMAGE_VERSION)\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.populateJail = \"$(IMAGE_REPO)/populate_jail:$(IMAGE_VERSION)\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.soperatorExporter = \"$(IMAGE_REPO)/soperator-exporter:$(IMAGE_VERSION)\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.sConfigController = \"$(IMAGE_REPO)/sconfigcontroller:$(OPERATOR_IMAGE_TAG)\"" "helm/slurm-cluster/values.yaml"
	@$(YQ) -i ".images.mariaDB = \"docker-registry1.mariadb.com/library/mariadb:11.4.3\"" "helm/slurm-cluster/values.yaml"
	@# endregion helm/slurm-cluster/values.yaml

	@# region helm/soperator-activechecks/values.yaml
	@echo 'Syncing helm/soperator-activechecks/values.yaml'
	@$(YQ) -i ".images.munge = \"$(IMAGE_REPO)/munge:$(IMAGE_VERSION)\"" "helm/soperator-activechecks/values.yaml"
	@$(YQ) -i ".images.k8sJob = \"$(IMAGE_REPO)/k8s_check_job:$(IMAGE_VERSION)\"" "helm/soperator-activechecks/values.yaml"
	@$(YQ) -i ".images.slurmJob = \"$(IMAGE_REPO)/slurm_check_job:$(IMAGE_VERSION)\"" "helm/soperator-activechecks/values.yaml"
	@# endregion helm/soperator-activechecks/values.yaml

	@# region helm/nodeconfigurator/values.yaml
	@echo 'Syncing helm/nodeconfigurator/values.yaml'
	@$(YQ) -i ".rebooter.image.repository = \"$(IMAGE_REPO)/rebooter\"" "helm/nodeconfigurator/values.yaml"
	@$(YQ) -i ".rebooter.image.tag = \"$(OPERATOR_IMAGE_TAG)\"" "helm/nodeconfigurator/values.yaml"
	@# endregion helm/nodeconfigurator/values.yaml

	@# region helm/soperatorchecks/values.yaml
	@echo 'Syncing helm/soperatorchecks/values.yaml'
	@$(YQ) -i ".checks.manager.image.repository = \"$(IMAGE_REPO)/soperatorchecks\"" "helm/soperatorchecks/values.yaml"
	@$(YQ) -i ".checks.manager.image.tag = \"$(OPERATOR_IMAGE_TAG)\"" "helm/soperatorchecks/values.yaml"
	@# endregion helm/soperatorchecks/values.yaml

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

	@# region fluxcd/environment/nebius-cloud/*/bootstrap/flux-kustomization.yaml
	@echo 'Syncing helm/soperator-fluxcd/values.yaml'
	@$(YQ) -i ".spec.postBuild.substitute.soperator_version = \"$(OPERATOR_IMAGE_TAG)\"" "fluxcd/environment/nebius-cloud/dev/bootstrap/flux-kustomization.yaml"
	@$(YQ) -i ".spec.postBuild.substitute.soperator_version = \"$(OPERATOR_IMAGE_TAG)\"" "fluxcd/environment/nebius-cloud/prod/bootstrap/flux-kustomization.yaml"
	@# endregion fluxcd/environment/nebius-cloud/*/bootstrap/flux-kustomization.yaml

	@# region helm/soperator-fluxcd/values.yaml
	@echo 'Syncing helm/soperator-fluxcd/values.yaml'
	@$(YQ) -i ".slurmCluster.version = \"$(OPERATOR_IMAGE_TAG)\"" "helm/soperator-fluxcd/values.yaml"
	@$(YQ) -i ".soperatorActiveChecks.version = \"$(OPERATOR_IMAGE_TAG)\"" "helm/soperator-fluxcd/values.yaml"
	@$(YQ) -i ".soperator.version = \"$(OPERATOR_IMAGE_TAG)\"" "helm/soperator-fluxcd/values.yaml"
	@$(YQ) -i ".observability.dcgmExporter.version = \"$(OPERATOR_IMAGE_TAG)\"" "helm/soperator-fluxcd/values.yaml"
	@$(YQ) -i ".notifier.version = \"$(OPERATOR_IMAGE_TAG)\"" "helm/soperator-fluxcd/values.yaml"
	@# endregion helm/soperator-fluxcd/values.yaml

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
sync-version-from-scratch: generate manifests helm mock sync-version ## Regenerates all resources and syncs versions to them

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary with native toolchain.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host with native toolchain.
	IS_PROMETHEUS_CRD_INSTALLED=true IS_MARIADB_CRD_INSTALLED=true ENABLE_WEBHOOKS=false IS_APPARMOR_CRD_INSTALLED=true go run cmd/main.go \
	 -log-level=debug -leader-elect=false -operator-namespace=soperator-system --enable-topology-controller=true

.PHONY: docker-build-go-base
docker-build-go-base: ## Build go-base multiarch manifest locally
# Build multiarch
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--target go-base \
		-t go-base \
		-f images/common/go-base.dockerfile \
		$(DOCKER_BUILD_ARGS) \
		.

.PHONY: docker-build-and-push
docker-build-and-push: ## Build and push docker multi arch manifest
ifndef IMAGE_NAME
	$(error IMAGE_NAME is not set)
endif
ifndef DOCKERFILE
	$(error DOCKERFILE is not set)
endif
ifndef UNSTABLE
	$(error UNSTABLE is not set)
endif
# Build multiarch
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--target ${IMAGE_NAME} \
		-t "$(IMAGE_REPO)/${IMAGE_NAME}:${IMAGE_VERSION}" \
		-f images/${DOCKERFILE} \
		--build-arg SLURM_VERSION="${SLURM_VERSION}" \
		--push \
		$(DOCKER_BUILD_ARGS) \
		.

ifeq ($(UNSTABLE), false)
	skopeo copy --all \
		docker://"$(IMAGE_REPO)/${IMAGE_NAME}:${IMAGE_VERSION}" \
		docker://"$(GITHUB_REPO)/${IMAGE_NAME}:${IMAGE_VERSION}"
endif

.PHONY: docker-build-jail
docker-build-jail: ## Build jail
ifndef IMAGE_VERSION
	$(error IMAGE_VERSION is not set, docker image cannot be built)
endif
# Output type tar doesn't support platform splitting, so we keep single arch build here.
	# Build amd
	docker buildx build \
		--platform linux/amd64 \
		--target jail \
		-t "$(IMAGE_REPO)/jail:${IMAGE_VERSION}-amd64" \
		-f images/jail/jail.dockerfile \
		--output type=tar,dest=images/jail_rootfs_amd64.tar \
		.

	# Build arm
	docker buildx build \
		--platform linux/arm64 \
		--target jail \
		-t "$(IMAGE_REPO)/jail:${IMAGE_VERSION}-arm64" \
		-f images/jail/jail.dockerfile \
		--output type=tar,dest=images/jail_rootfs_arm64.tar \
		.

.PHONY: docker-manifest
docker-manifest: ## Create and push docker manifest for multiple image architecture
ifndef IMAGE_NAME
	$(error IMAGE_NAME is not set, docker manifest can not be pushed)
endif
	docker manifest create --amend "$(IMAGE_REPO)/${IMAGE_NAME}:${IMAGE_VERSION}" "$(IMAGE_REPO)/${IMAGE_NAME}:${IMAGE_VERSION}-arm64" "$(IMAGE_REPO)/${IMAGE_NAME}:${IMAGE_VERSION}-amd64"
	docker manifest push "$(IMAGE_REPO)/${IMAGE_NAME}:${IMAGE_VERSION}"
ifeq ($(UNSTABLE), false)
	docker manifest create --amend "$(GITHUB_REPO)/${IMAGE_NAME}:${IMAGE_VERSION}" "$(GITHUB_REPO)/${IMAGE_NAME}:${IMAGE_VERSION}-arm64" "$(GITHUB_REPO)/${IMAGE_NAME}:${IMAGE_VERSION}-amd64"
	docker manifest push "$(GITHUB_REPO)/${IMAGE_NAME}:${IMAGE_VERSION}"
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
MOCKERY        ?= $(LOCALBIN)/mockery

## Tool Versions
KUSTOMIZE_VERSION        ?= v5.5.0
CONTROLLER_TOOLS_VERSION ?= v0.16.4
ENVTEST_VERSION          ?= release-0.17
GOLANGCI_LINT_VERSION    ?= v2.0.2  # Should be in sync with the github CI step.
HELMIFY_VERSION          ?= 0.4.13
HELM_VERSION						 ?= v3.18.3
HELM_UNITTEST_VERSION    ?= 0.8.2
YQ_VERSION               ?= 4.44.3
MOCKERY_VERSION 		 ?= 2.53.4

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || GOBIN=$(LOCALBIN) GO111MODULE=on go install sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION)

.PHONY: mockery
mockery:
	@mkdir -p $(LOCALBIN)
	@current_version="$$( $(MOCKERY) --version 2>&1 | grep -o 'version=v[0-9.]*' | cut -d= -f2 || true)"; \
	if [ "$$current_version" != "v$(MOCKERY_VERSION)" ]; then \
		echo "🛠  Installing mockery v$(MOCKERY_VERSION) (found: $$current_version)"; \
		rm -f $(MOCKERY); \
		GOBIN=$(LOCALBIN) GO111MODULE=on go install github.com/vektra/mockery/v2@v$(MOCKERY_VERSION); \
	else \
		echo "✅ mockery v$(MOCKERY_VERSION) already installed"; \
	fi

.PHONY: mock
mock: mockery ## Generate mocks using mockery
	$(MOCKERY)

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

.PHONY: helmtest check-helm install-helm install-unittest

## helm unittest: Run helm unittest with dependency check
helmtest: check-helm
	@echo "Running helm unittest"
	@helm unittest $(CHART_PATH)/soperator-fluxcd
	@helm unittest $(CHART_PATH)/slurm-cluster
	@helm unittest $(CHART_PATH)/slurm-cluster-storage
	@helm unittest $(CHART_PATH)/soperator-notifier

check-helm:
	@echo "Checking Helm installation..."
	@if ! command -v helm >/dev/null 2>&1; then \
		echo "Helm not found, installing..."; \
		$(MAKE) install-helm; \
	else \
		echo "Helm found: $$(helm version --short)"; \
	fi
	@echo "Checking helm-unittest plugin..."
	@if ! helm plugin list 2>/dev/null | grep -q unittest; then \
		echo "helm-unittest plugin not found, installing..."; \
		$(MAKE) install-unittest; \
	else \
		echo "helm-unittest plugin found"; \
	fi

install-helm:
	@echo "Installing Helm $(HELM_VERSION)..."
	@curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash

install-unittest:
	@echo "Installing helm-unittest plugin $(HELM_UNITTEST_VERSION)..."
	@helm plugin install https://github.com/helm-unittest/helm-unittest --version $(HELM_UNITTEST_VERSION)
