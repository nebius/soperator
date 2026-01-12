#!/bin/bash
# Retry wrapper with exponential backoff
# Usage: ./retry.sh [options] -- command [args...]
#   -n MAX_ATTEMPTS  Maximum retry attempts (default: 3)
#   -d DELAY         Initial delay in seconds (default: 5)

set -euo pipefail

MAX_ATTEMPTS=3
DELAY=5

while getopts "n:d:" opt; do
  case $opt in
    n) MAX_ATTEMPTS=$OPTARG ;;
    d) DELAY=$OPTARG ;;
    *) echo "Usage: $0 [-n max_attempts] [-d delay] -- command"; exit 1 ;;
  esac
done
shift $((OPTIND - 1))
[[ "${1:-}" == "--" ]] && shift

attempt=1
while true; do
  if "$@"; then
    exit 0
  fi

  if [[ $attempt -ge $MAX_ATTEMPTS ]]; then
    echo "Command failed after $MAX_ATTEMPTS attempts: $*"
    exit 1
  fi

  echo "Attempt $attempt/$MAX_ATTEMPTS failed. Retrying in ${DELAY}s..."
  sleep "$DELAY"
  DELAY=$((DELAY * 2))
  attempt=$((attempt + 1))
done
