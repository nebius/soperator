#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

usage() {
    echo "Get the order of nodes in which Slurm assigns node ranks to jobs." >&2
    echo "This order is relevant in case network topology is used, and the node distribution method is \"block\"." >&2
    echo "" >&2
    echo "usage: ${0} [-c] [-w nodelist] [-j job_id] [-h]" >&2
    echo "       exactly one of -c, -w, or -j must be specified"
    echo "  -c: show the order of all nodes in the cluster"
    echo "  -w <nodelist>: show the order of a specific <nodelist> (e.g. worker-[1-10])"
    echo "  -j <job_id>: show the order of the nodes allocated to <job_id>"
    exit 1
}

all_nodes=false
nodelist=""
job_id=""

while getopts cw:j:h flag
do
    case "${flag}" in
        c) all_nodes=true;;
        w) nodelist=${OPTARG};;
        j) job_id=${OPTARG};;
        h) usage;;
        *) usage;;
    esac
done

# Validate arguments
count=0
$all_nodes && count=$((count+1))
[ -n "$nodelist" ] && count=$((count+1))
[ -n "$job_id" ] && count=$((count+1))
[ "$count" -eq 1 ] || usage

if [ -n "$job_id" ]; then
    if ! out=$(scontrol show job "$job_id" --json | jq -r '.jobs[0].nodes'); then
        echo "Failed to get nodelist for job $job_id" >&2
        exit 1
    fi
    nodelist="$out"
fi

if ! hostnames=$(scontrol show hostnames "$nodelist"); then
    echo "Failed to get hostnames for nodelist $nodelist" >&2
    exit 1
fi

if ! cluster_order=$(scontrol show topology \
    | awk -F'Nodes=' '/Level=0/ {print $2}' \
    | tr ',' '\n'); then
    echo "Failed to get network topology" >&2
    exit 1
fi

if $all_nodes; then
    nodelist_order=$cluster_order
else
    if ! nodelist_order=$(printf '%s\n' "$cluster_order" \
        | grep -F -x -f <(printf '%s\n' "$hostnames")); then
        echo "Failed to filter cluster hostnames"
        exit 1
    fi
fi

printf '%s\n' "$nodelist_order" \
    | awk '{print NR-1, $0}'
