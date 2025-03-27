#!/usr/bin/env bash

set -e

SCRIPT_DIR=$(dirname "$(readlink -f "$0")")
flux install --export > $SCRIPT_DIR/resources.yaml
