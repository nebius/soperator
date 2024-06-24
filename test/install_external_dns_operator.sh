#!/bin/bash

export HELM_EXPERIMENTAL_OCI=1

helm pull oci://cr.yandex/crpm100b5l6ijkjedk3l/yandex-cloud/externaldns/helm/externaldns \
  --version 0.5.0 \
  --untar

helm install --kube-context ncp-slurm-operator \
  --namespace external-dns \
  --create-namespace \
  --set config.folder_id=bje82q7sm8njm3c4rrlq \
  --set config.api_server_url="api.nemax.nebius.cloud:443" \
  --set-file config.auth.json=/Users/rodrijjke/Documents/slurm/slurm-poc-key.json \
  externaldns ./externaldns/
