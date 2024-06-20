#!/bin/bash

# Current command:
# ./attach_filestores_to_nogpu_node_group.sh -p nemax -i a4hgk31npft3iqv5vrn7 -n f2md1q6j5mq7mju9agk2 -c dp7tjam8j7qasd0fecuv -j dp7rvn0cqokv5ijj190a

usage() { echo "usage: ${0} -p <ycp_profile> -i <instance_group_id> -n <node_group_id> -c <controller_spool_filestore_id> -j <jail_filestore_id> [-h]" >&2; exit 1; }

while getopts p:i:n:c:j:h flag
do
    case "${flag}" in
        p) profile=${OPTARG};;
        i) instance_group_id=${OPTARG};;
        n) node_group_id=${OPTARG};;
        c) controller_spool_filestore_id=${OPTARG};;
        j) jail_filestore_id=${OPTARG};;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "$profile" ] || [ -z "$instance_group_id" ] || [ -z "$node_group_id" ] || \
   [ -z "$controller_spool_filestore_id" ] || [ -z "$jail_filestore_id" ]; then
    usage
fi

ycp --profile "${profile}" microcosm instance-group --id "${instance_group_id}" update --referrer-id "${node_group_id}" -r - <<EOF
update_mask:
  paths:
  - instance_template.filesystem_specs

instance_template:
  filesystem_specs:
    - mode: READ_WRITE
      device_name: controller-spool
      filesystem_id: $controller_spool_filestore_id
    - mode: READ_WRITE
      device_name: jail
      filesystem_id: $jail_filestore_id
EOF
