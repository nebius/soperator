#!/bin/bash

# Example: ./release_helm.sh -v version -u repo_uri

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

usage() {
  echo "usage: ${0} -u <repo_uri> [-h]"
  echo "  -u <repo_uri> Container registry repo"
  echo "  -h            Print this help message"
}

while getopts "u:h" flag
do
  case "${flag}" in
    u) repo_uri=${OPTARG};;
    h) usage; exit 0;;
    *) usage; exit 1;;
  esac
done

if [ -z "${repo_uri}" ]; then
  echo "Error: repo_uri must be set. Use -u <repo_uri> to specify the repository URI."
  exit 1
fi

HELM_PATH="helm"
RELEASE_PATH="helm-releases"

mkdir -p $RELEASE_PATH

chart() {
  CHART_NAME=$1
  shift 1
  OCI_REPO=$1
  shift 1

  CHART_VERSION=$(grep '^version:' "${HELM_PATH}/${CHART_NAME}/Chart.yaml" | awk '{print $2}' | sed 's/"//g')

  if [ -z "${CHART_VERSION}" ]; then
    echo "Error: Could not find version in ${HELM_PATH}/${CHART_NAME}/Chart.yaml"
    exit 1
  fi

  CHART_TARGET="${CHART_NAME}-${CHART_VERSION}.tgz"

  echo "Updating dependencies of chart ${CHART_NAME}."
  helm dependency update "${HELM_PATH}/${CHART_NAME}"

  echo "Packaging chart ${CHART_NAME} (version ${CHART_VERSION}) as ${CHART_TARGET}."
  helm package -d "${RELEASE_PATH}" "${HELM_PATH}/${CHART_NAME}"

  echo "Pushing helm-${CHART_TARGET} to Container registry - ${OCI_REPO}"
  "${SCRIPT_DIR}/scripts/retry.sh" -n 3 -d 5 -- helm push "${RELEASE_PATH}/helm-${CHART_TARGET}" "${OCI_REPO}"

  echo '---'
}

for dirpath in "${HELM_PATH}"/* ; do
      [ ! -d "${dirpath}" ] && continue
      chart "$(basename "${dirpath}")" "oci://${repo_uri}"
done

rm -rf ${RELEASE_PATH}
