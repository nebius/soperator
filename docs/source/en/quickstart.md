---
title: "Getting started with {{ slurm-operator }}"
description: "In this tutorial, you will learn how to create a Slurm cluster in k8s with {{ slurm-operator }}."
---

# Getting started with {{ slurm-operator }}

# How to create a Slurm cluster
The creation process consists of the following steps:
1. Download the terraform and helm charts. At this moment, you can just download the latest tarball from [this directory](https://arcanum.nebius.dev/nebo/msp/slurm-service/internal/operator/terraform-releases).
2. Copy the tarball from Downloads into some other directory, if needed: `cp ~/Downloads/slurm_operator_tf_<version>.tar.gz /path/to/some/dir && cd /pat/to/some/dir`
3. Initialise the terraform: `terraform init`. This command downloads all required modules.
3. Fill out terraform variables in `terraform.tfvars`. You can use the `terraform.tfvars.example` file as a reference. See comments to the fields in order to set them correctly.
4. Exec `terraform apply` in order to create all necessary resources.

The terraform creates the following:
- K8S cluster
- VPC (if `k8s_network_id` variable is not set, otherwise, it uses an existing one)
- A static IP address in this network
- Shared storage where the slurm nodes' root directory will be stored. Either a GlusterFS cluster run on a bunch of compute instances, or a compute file storage.
- A compute file storage for storing Slurm controller state in it and share between primary and backup controllers.
- The configured number of additional compute file storages that the user wants to mount into their environment.
- Several Helm releases:
    - NVIDIA GPU operator, that propagates GPU drivers and low-level libraries from K8S nodes into containers
    - NVIDIA network operator, that propagates InfiniBand drivers and low-level libraries from K8S nodes into containers
    - Slurm operator, that creates Slurm clusters
    - Slurm cluster storage, that brings the shared storages (GlusterFS and/or compute file storage) from K8S 
