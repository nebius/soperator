SHELL = bash

ARCH ?= $(shell arch)
ifeq ($(ARCH),arm64)
ARCH_DOCKER = arm64v8
else
ARCH_DOCKER = amd64
endif

# Or `debug`
TARGET_COMPILATION_MODE ?= release

# https://github.com/SchedMD/slurm/tags
SLURM_VERSION ?= 24.11.5.1
# Replace underscores and dashes with dots
SLURM_VERSION_BRANCH = $(subst _,-,$(subst .,-,$(SLURM_VERSION)))

SLURM_HEADERS_DIR = vendor/slurm


all: build


.PHONY: headers
headers: $(SLURM_HEADERS_DIR) docker-slurm-base
	@docker build \
		-t spanknccldebug-headers \
		-f docker/headers.dockerfile \
		--platform linux/$(ARCH) \
		--build-arg ARCH=$(ARCH_DOCKER) \
		--build-arg SLURM_VERSION=$(SLURM_VERSION_BRANCH) \
		.

	@docker run \
		-v '$(shell pwd)/$(SLURM_HEADERS_DIR):/tmp/include/slurm' \
		--rm \
		--platform linux/$(ARCH) \
		spanknccldebug-headers


$(SLURM_HEADERS_DIR):
	@mkdir -p $@


.PHONY: build
build: docker-slurm-base
	@docker build \
		-t spanknccldebug-builder \
		-f docker/builder.dockerfile \
		--platform linux/$(ARCH) \
		--build-arg ARCH=$(ARCH_DOCKER) \
		--build-arg SLURM_VERSION=$(SLURM_VERSION_BRANCH) \
		--build-arg MODE=$(TARGET_COMPILATION_MODE) \
		.

	@docker run \
		-v '$(shell pwd)/build:/usr/src/spanknccldebug/build' \
		--rm \
		--platform linux/$(ARCH) \
		spanknccldebug-builder


.PHONY: docker-slurm-base
docker-slurm-base:
	@docker build \
		-t spanknccldebug-base \
		-f docker/base.dockerfile \
		--platform linux/$(ARCH) \
		--build-arg ARCH=$(ARCH_DOCKER) \
		--build-arg SLURM_VERSION=$(SLURM_VERSION_BRANCH) \
		.


.PHONY: redeploy
redeploy: _recreate_configmap _restart_pods _remount_lib ;


.PHONY: _recreate_configmap _restart_pods _remount_lib
_recreate_configmap:
	@kubectl -n soperator delete configmap spanknccldebug && \
	kubectl -n soperator create configmap spanknccldebug --from-file build/spanknccldebug.so

_restart_pods:
	@kubectl -n soperator delete pod login-0
	@kubectl -n soperator delete pod worker-0

	@echo -n 'Waiting for worker-0 to be Running'
	@until kubectl -n soperator get pod worker-0 -o jsonpath='{.status.phase}' 2>/dev/null | grep -q Running; do \
		echo -n '.'; sleep 2; \
	done;
	@echo '' && echo -n 'Waiting for login-0 to be Running'
	@until kubectl -n soperator get pod login-0 -o jsonpath='{.status.phase}' 2>/dev/null | grep -q Running; do \
		echo -n '.'; sleep 2; \
	done;
	@echo ''

_remount_lib:
	kubectl -n soperator exec worker-0 -c slurmd -- mount --bind /usr/lib/x86_64-linux-gnu/slurm/spanknccldebug.so /mnt/jail/usr/lib/x86_64-linux-gnu/slurm/spanknccldebug.so
	kubectl -n soperator exec login-0  -c sshd   -- mount --bind /usr/lib/x86_64-linux-gnu/slurm/spanknccldebug.so /mnt/jail/usr/lib/x86_64-linux-gnu/slurm/spanknccldebug.so
