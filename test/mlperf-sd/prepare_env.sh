#!/bin/bash

set -e

mkdir -p /mlperf-sd
cd /mlperf-sd

echo "Checkout MLCommons git repo"
export GIT_DISCOVERY_ACROSS_FILESYSTEM=1
git clone --depth=1 https://github.com/mlcommons/training
pushd /mlperf-sd/training
    git fetch --depth=1 origin 00f04c57d589721aabce4618922780d29f73cf4e
    git checkout 00f04c57d589721aabce4618922780d29f73cf4e
popd

echo "Adjust some files"
mkdir -p /mlperf-sd/data/results
mkdir -p /mlperf-sd/hf_home
cp -r /opt/mlperf-sd/training        /mlperf-sd/
cp -r /opt/mlperf-sd/aws_download.sh /mlperf-sd/

chown -R 0:0 /mlperf-sd

echo "Install awscli"
apt update && apt install -y awscli

echo "Start batch job for downloading datasets & results"
sbatch aws_download.sh

echo "Started job aws_download"
squeue
