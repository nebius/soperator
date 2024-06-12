#!/bin/bash

# Current command:
# ./attach_filestores_to_gpu_node_group.sh -p nemax -i a4hbbuedo27opi4imnej -n f2mek81b68gi5k66dsfg -j dp7rvn0cqokv5ijj190a

usage() { echo "usage: ${0} -p <ycp_profile> -i <instance_group_id> -n <node_group_id> -j <jail_filestore_id> [-h]" >&2; exit 1; }

while getopts p:i:n:j:h flag
do
    case "${flag}" in
        p) profile=${OPTARG};;
        i) instance_group_id=${OPTARG};;
        n) node_group_id=${OPTARG};;
        j) jail_filestore_id=${OPTARG};;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "$profile" ] || [ -z "$instance_group_id" ] || [ -z "$node_group_id" ] || [ -z "$jail_filestore_id" ]; then
    usage
fi

ycp --profile "${profile}" microcosm instance-group --id "${instance_group_id}" update --referrer-id "${node_group_id}" -r - <<EOF
update_mask:
  paths:
  - instance_template.filesystem_specs

instance_template:
  filesystem_specs:
    - mode: READ_WRITE
      device_name: jail
      filesystem_id: $jail_filestore_id
EOF
