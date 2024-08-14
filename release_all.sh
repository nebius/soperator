#!/bin/bash

set -e

usage() { echo "usage: ${0} [-s] -u <ssh_user> -k <path_to_ssh_key> -a <address_of_build_agent> [-h]" >&2; exit 1; }

while getopts u:k:a:sh flag
do
    case "${flag}" in
        u) user=${OPTARG};;
        k) key=${OPTARG};;
        a) address=${OPTARG};;
        s) stable="1";;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "$user" ] || [ -z "$key" ] || [ -z "$address" ]; then
    usage
fi



start_time=$(date +%s)

if [ "$stable" == "1" ]; then
    UNSTABLE="false"
else
    UNSTABLE="true"
fi

echo "Syncing versions among all files for stable release"
make sync-version UNSTABLE=${UNSTABLE}
IMAGE_VERSION=$(make get-image-version UNSTABLE=${UNSTABLE})
VERSION=$(make get-version UNSTABLE=${UNSTABLE})

echo "Version is ${VERSION}"
echo "Image version is ${IMAGE_VERSION}"

echo "Uploading images to the build agent"
pushd images
    ./upload_to_build_agent.sh -u "$user" -k "$key"
popd

remote_command=$(cat <<EOF
set -e
set -x

echo "Entering /usr/src/prototypes/slurm/${user}"
cd "/usr/src/prototypes/slurm/${user}"
sudo su -- <<'ENDSSH'

echo "Remove previous outputs"
rm -rf outputs/*

echo "Building container images"
./build_common.sh
IMAGE_VERSION=${IMAGE_VERSION} ./build_all.sh -s "${stable}" &
IMAGE_VERSION=${IMAGE_VERSION} ./build_populate_jail.sh -s "${stable}" &

wait

echo "Parsing build outputs"
RED='\033[0;31m'
GREEN='\033[0;32m'
RESET='\033[0m'
for log_file in outputs/*; do
    if [ -f "\$log_file" ]; then
        last_line="\$(tail -n 1 \$log_file)"
        if [ "\${last_line}" == "OK" ]; then
            echo -e "\${GREEN}\${log_file} is OK\${RESET}"
        else
            echo -e "\${RED}\${log_file} is NOT OK\${RESET}"
            exit 1
        fi
    fi
done
ENDSSH
EOF
)

ssh -i "$key" "$user"@"$address" "${remote_command}"
echo "All images are built successfully"

echo "Updating CRDs & auto-generated code (included in test step) & run tests"
make test UNSTABLE=${UNSTABLE}

echo "Building image of the operator"
make docker-push UNSTABLE=${UNSTABLE}

echo "Pusing Helm charts"
./release_helm.sh -afyr -v ${VERSION}

echo "Packing new terraform tarball"
VERSION=${VERSION} ./release_terraform.sh -f

echo "Unpacking the terraform tarball"
version_formatted=$(echo "${VERSION}" | tr '-' '_' | tr '.' '_')
tarball="slurm_operator_tf_$version_formatted.tar.gz"

pushd ./terraform-releases/unstable
    VERSION=${VERSION} TARBALL=${tarball} ./unpack_current_version.sh
popd

GREEN='\033[0;32m'
RESET='\033[0m'

if [ "$stable" == "1" ]; then
    mv "terraform-releases/unstable/$tarball" "terraform-releases/stable/"
    echo -e "${GREEN}Stable version '$VERSION' is successfully released${RESET}"
else
    echo -e "${GREEN}Unstable version '$VERSION' is successfully released and unpacked to terraform-releases/unstable/${RESET}"
fi

end_time=$(date +%s)
duration=$((end_time - start_time))

echo "Time taken: ${duration} seconds"
