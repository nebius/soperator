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
SLURM_VERSION ?= 24.11.6.1
# Replace underscores and dashes with dots
SLURM_VERSION_BRANCH = $(subst _,-,$(subst .,-,$(SLURM_VERSION)))

SLURM_HEADERS_DIR = vendor/slurm

NODE_COUNT_WORKER ?= 1
NODE_COUNT_LOGIN ?= 1
NODE_RANGE_WORKER := $(shell seq 0 $$(($(NODE_COUNT_WORKER) - 1)))
NODE_RANGE_LOGIN := $(shell seq 0 $$(($(NODE_COUNT_LOGIN) - 1)))


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
redeploy:
	$(foreach \
		i, \
		$(NODE_RANGE_WORKER), \
		kubectl -n soperator cp ./build/spanknccldebug.so worker-$(i):/usr/lib/x86_64-linux-gnu/slurm/ ; \
	)
	$(foreach \
		i, \
		$(NODE_RANGE_LOGIN), \
		kubectl -n soperator cp ./build/spanknccldebug.so login-$(i):/usr/lib/x86_64-linux-gnu/slurm/ ; \
	)
