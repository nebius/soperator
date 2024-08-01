#!/usr/bin/env bash
set -e

export NCP_TOKEN=$(ncp iam create-token)
export NCP_CLOUD_ID=$(ncp config get cloud-id)
export NCP_FOLDER_ID=$(ncp config get folder-id)
export SECRET_ID=cd46hgrbncplpekv32tk

export  JSON_PAYLOAD=$(ncp lockbox payload get --id "${SECRET_ID}" --format=json)
export  AWS_ACCESS_KEY_ID=$(echo $JSON_PAYLOAD | jq -r '.entries[] | select(.key == "key_id") | .text_value')
export  AWS_SECRET_ACCESS_KEY=$(echo $JSON_PAYLOAD | jq -r '.entries[] | select(.key == "secret") | .text_value')


VERB=${1}
if [ -z "${VERB}" ]; then
    echo "Error: missing VERB paramter"
    echo "Usage: ./deploy.sh <VERB>"
    exit 1
fi

terraform -chdir="./terraform/oldbius" init \
    -backend-config="bucket=terraform-state-slurm" \
    -backend-config="key=slurm-dev" \
    -backend-config="region=eu-north1" \
    -backend-config="endpoint=storage.nemax.nebius.cloud" \
    -backend-config="skip_region_validation=true" \
    -backend-config="skip_credentials_validation=true"


if [ ${VERB} == "plan" ]; then
  terraform -chdir="./terraform/oldbius" plan -input=false
fi
if [ ${VERB} == "apply" ]; then
  terraform -chdir="./terraform/oldbius" apply -input=false
fi

echo "finished"
