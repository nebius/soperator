#!/bin/bash

pushd /tmp || exit 1
  curl "https://rclone.org/install.sh" | bash
  rm -rf /tmp/*
popd || exit 1
