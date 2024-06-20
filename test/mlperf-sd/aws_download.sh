#!/bin/bash

#SBATCH -J aws_download
#SBATCH --output=/mlperf-sd/aws_download.log
#SBATCH --error=/mlperf-sd/aws_download.log

srun aws s3 --endpoint-url=https://storage.ai.nebius.cloud cp --no-sign-request --recursive s3://gpt3/stable-diff /mlperf-sd
