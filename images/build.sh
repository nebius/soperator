#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

usage() { echo "usage: ${0} -t <image_tag> [-d <dockerfile>] [-i] [-n][-h]" >&2; exit 1; }

while getopts t:d:m:d:inh flag
do
    case "${flag}" in
        t) tag=${OPTARG};;
        i) ignore_cache=--no-cache;;
        n) no_push=1;;
        d) dockerfile=${OPTARG};;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "$tag" ] || [ -z "$dockerfile" ]; then
    usage
fi

# https://console.nebius.ai/folders/bjef05jvuvmaf2mmuckr/container-registry/registries/crnonjecps8pifr7am4i/overview
container_registry_id=crnonjecps8pifr7am4i

docker build --tag "${tag}" --target "${tag}" $ignore_cache --load --platform=linux/amd64 -f "${dockerfile}" .

echo "Built image ${tag}"

if [ -z $no_push ]; then
    docker tag "${tag}" cr.ai.nebius.cloud/"${container_registry_id}"/"${tag}"
    docker push cr.ai.nebius.cloud/"${container_registry_id}"/"${tag}"
    echo "Pushed image ${tag}"
fi
