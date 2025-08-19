#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

# ALT_ARCH has the extended form like: x86_64, aarch64
ALT_ARCH="$(uname -m)"

pushd /tmp || exit 1
  curl "https://awscli.amazonaws.com/awscli-exe-linux-${ALT_ARCH}.zip" -o "awscliv2.zip"
  unzip awscliv2.zip
  ./aws/install
  rm -rf /tmp/*
popd || exit 1

rm -rf /usr/local/aws-cli/v2/*/dist/awscli/examples
