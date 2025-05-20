#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

pushd /tmp || exit 1
  curl "https://rclone.org/install.sh" | bash
  rm -rf /tmp/*
popd || exit 1
