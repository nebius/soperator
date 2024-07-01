#!/bin/bash

set -e

mkdir -p /mlperf-sd

pushd /mlperf-sd
    echo "Checkout MLCommons git repo"
    export GIT_DISCOVERY_ACROSS_FILESYSTEM=1
    git clone https://github.com/mlcommons/training
    pushd /mlperf-sd/training
        git checkout 00f04c57d589721aabce4618922780d29f73cf4e
    popd

    echo "Copy and replace some files"
    cp -r /tmp/mlperf-sd/data            /mlperf-sd/
    cp -r /tmp/mlperf-sd/hf_home         /mlperf-sd/
    cp -r /tmp/mlperf-sd/training        /mlperf-sd/
    cp -r /tmp/mlperf-sd/aws_download.sh /mlperf-sd/

    chown -R 0:0 /mlperf-sd

    echo "Install awscli"
    apt update && apt install -y awscli

    echo "Start batch job for downloading datasets & results"
    sbatch aws_download.sh
popd
