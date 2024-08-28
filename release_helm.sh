#!/bin/bash

# Example: ./release_helm.sh -afyr

set -e

usage() {
  echo "usage: ${0} [-n <chart-name> | -a] [-v <version>] [-f] [-y] [-r] [-h]"
  echo "  Mutually exclusive chart name options. One of required"
  echo "    -n <chart-name>	Use particular chart name"
  echo "    -a             	Use all chart names"
  echo ""
  echo "  -v <version>	Override chart version"
  echo "  -f          	Force-rewrite existing archive"
  echo "  -y          	Do not ask for confirmation"
  echo "  -r          	Whether to release packaged chart to the registry"
  echo ""
  echo "  -h	Print this help message"
}

while getopts n:av:fyrh flag
do
  case "${flag}" in
    n) name=${OPTARG};;
    a) all=1;;
    v) version=${OPTARG};;
    f) force=1;;
    y) force_yes=1;;
    r) release=1;;
    h) usage && exit;;
    *) usage && exit 1;
  esac
done

if { [ -n "${name}" ] && [ -n "${all}" ]; } || { [ -z "${name}" ] && [ -z "${all}" ]; }; then
  echo 'One of -n or -a flags must be provided.'
  exit 1
fi

HELM_PATH="helm"
RELEASE_PATH="helm-releases"

mkdir -p $RELEASE_PATH

# https://console.nebius.ai/folders/bje82q7sm8njm3c4rrlq/container-registry/registries/crnefnj17i4kqgt3up94/overview
CONTAINER_REGISTRY_ID='crnefnj17i4kqgt3up94'

chart() {
  CHART_NAME=${1} && shift 1
  CHART_VERSION=${1} && shift 1
  CHART_TARGET="${CHART_NAME}-${CHART_VERSION}.tgz"

  if [ -f "${RELEASE_PATH}/${CHART_TARGET}" ] && [ -z "${force}" ]; then
    echo "${CHART_TARGET} already exists. Use -f to override existing file."
    exit 1
  fi

  if [ -z "${release}" ]; then
    echo "Packaging chart ${CHART_NAME} as ${CHART_TARGET}."
  else
    echo "Packaging and releasing chart ${CHART_NAME} as ${CHART_TARGET}."
  fi

  if [ -z "${force_yes}" ]; then
    read -rp 'Do you want to proceed? (y/n): ' yn
      case $yn in
        y) echo "OK.";;
        n) echo "Skipping ${CHART_NAME}..."; return;;
        *) echo "Invalid response. Skipping"; return;;
      esac
  fi

  helm package -d "${RELEASE_PATH}" "${HELM_PATH}/${CHART_NAME}"

  if [ -n "${release}" ]; then
    echo "Pushing ${CHART_TARGET} to Container registry..."
    helm push "${RELEASE_PATH}/${CHART_TARGET}" "oci://cr.ai.nebius.cloud/${CONTAINER_REGISTRY_ID}"
  fi

  echo '---'
}

if [ -n "${name}" ]; then
  if [ ! -d "${HELM_PATH}/${name}" ]; then
    echo "Chart with name '${name}' does not exist."; exit 1
  fi

  [ -z "${version}" ] && version=$(cat VERSION)
  chart "${name}" "${version}"
fi

if [ -n "${all}" ]; then
  for dirpath in "${HELM_PATH}"/* ; do
      [ ! -d "${dirpath}" ] && continue
      [ -z "${version}" ] && version=$(cat VERSION)
      chart "$(basename "${dirpath}")" "${version}"
  done
fi
