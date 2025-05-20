#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

pushd /tmp || exit 1
  curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
  unzip awscliv2.zip
  ./aws/install
  rm -rf /tmp/*
popd || exit 1
