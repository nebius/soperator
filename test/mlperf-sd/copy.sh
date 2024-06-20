#!/bin/bash

set -e

mkdir -p /mlperf-sd

pushd /mlperf-sd
    echo "Checkout MLCommons git repo"
    git clone https://github.com/mlcommons/training
    git checkout 00f04c57d589721aabce4618922780d29f73cf4e

    echo "Copy and replace some files"
    cp -r /tmp/mlperf-sd/data  /mlperf-sd/
    cp -r /tmp/hw_home         /mlperf-sd/
    cp -r /tmp/training        /mlperf-sd/
    cp -r /tmp/aws_download.sh /mlperf-sd/

    echo "Install awscli"
    apt update && apt install -y awscli

    echo "Start batch job for downloading datasets & results"
    sbatch aws_download.sh
popd
