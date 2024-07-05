#!/bin/bash

: "${NUM_NODES:=2}"
: "${GPUS_PER_NODE:=8}"
: "${CONFIG:=./configs/train_02x08x08.yaml}"
: "${WORKDIR:=/workdir}"
: "${RESULTS_MNT:=/results}"
: "${MOUNTS:=/dev/infiniband:/dev/infiniband}"
: "${CONTAINER_IMAGE:=mlperf_sd:22.12-py3}"
: "${CHECKPOINT:=/checkpoints/sd/512-base-ema.ckpt}"

while [ "$1" != "" ]; do
    case $1 in
        --num-nodes )       shift
                            NUM_NODES=$1
                            ;;
        --gpus-per-node )   shift
                            GPUS_PER_NODE=$1
                            ;;
        --config )          shift
                            CONFIG=$1
                            ;;
        --checkpoint )      shift
                            CHECKPOINT=$1
                            ;;
        --workdir )         shift
                            WORKDIR=$1
                            ;;
        --results-dir )         shift
                            RESULTS_MNT=$1
                            ;;
        --mounts )          shift
                            MOUNTS=$1
                            ;;
        --container )       shift
                            CONTAINER_IMAGE=$1
                            ;;
    esac
    shift
done

HF_HUB_OFFLINE=1
export HF_HUB_OFFLINE=1
NCCL_TOPO_FILE=/var/run/nvidia-topologyd/virtualTopology.xml
export NCCL_TOPO_FILE=/var/run/nvidia-topologyd/virtualTopology.xml
NCCL_DEBUG=INFO
export NCCL_DEBUG=INFO

srun \
    --container-image="${CONTAINER_IMAGE}" \
    --container-writable \
    --container-mounts="${MOUNTS}" \
    --container-workdir="${WORKDIR}" \
    --ntasks-per-node="${GPUS_PER_NODE}" \
    --cpus-per-task=16 \
    --mem-per-cpu="8G" \
    --nodes="${NUM_NODES}" \
    bash -c  "./run_and_time.sh \
    		--export NCCL_TOPO_FILE,HF_HUB_OFFLINE,NCCL_DEBUG \
                --num-nodes ${NUM_NODES} \
                --gpus-per-node ${GPUS_PER_NODE} \
                --checkpoint ${CHECKPOINT} \
                --results-dir ${RESULTS_MNT}  \
                --config ${CONFIG}"
