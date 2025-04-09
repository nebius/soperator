#!/bin/bash

# Example: ./release_helm.sh -v version -u repo_uri

set -e

usage() {
  echo "usage: ${0} [-v <version>] -u <repo_uri> [-h]"
  echo "  -v <version>	Chart version"
  echo "  -u <repo_uri> Container registry repo"
  echo "  -h            Print this help message"
}

while getopts "v:u:h" flag
do
  case "${flag}" in
    v) version=${OPTARG};;
    u) repo_uri=${OPTARG};;
    h) usage; exit 0;;
    *) usage; exit 1;;
  esac
done

if [ -z "${version}" ] || [ -z "${repo_uri}" ]; then
  echo "Error: Both repo_uri and version must be set. Use -u <repo_uri> to specify the repository URI and -v <version> to specify the version."
  exit 1
fi

HELM_PATH="helm"
RELEASE_PATH="helm-releases"

mkdir -p $RELEASE_PATH

chart() {
  CHART_NAME=$1
  shift 1
  CHART_VERSION=$1
  shift 1
  OCI_REPO=$1
  shift 1
  CHART_TARGET="${CHART_NAME}-${CHART_VERSION}.tgz"

  echo "Updating dependencies of chart ${CHART_NAME}."
  helm dependency update "${HELM_PATH}/${CHART_NAME}"

  echo "Packaging chart ${CHART_NAME} as ${CHART_TARGET}."
  helm package -d "${RELEASE_PATH}" "${HELM_PATH}/${CHART_NAME}"

  echo "Pushing helm-${CHART_TARGET} to Container registry - ${OCI_REPO}"
  helm push "${RELEASE_PATH}/helm-${CHART_TARGET}" "${OCI_REPO}"

  echo '---'
}

for dirpath in "${HELM_PATH}"/* ; do
      [ ! -d "${dirpath}" ] && continue
      chart "$(basename "${dirpath}")" "${version}" "oci://${repo_uri}"
done

rm -rf ${RELEASE_PATH}
