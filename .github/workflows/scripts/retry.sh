#!/bin/bash

set -e          # Exit on error
set -u          # Exit on undefined variable
set -o pipefail # Exit on pipe failure

# Generic retry wrapper for flaky commands used in CI workflow steps.
#
# Usage:
#   retry.sh <cmd> [args...]
#
# Env overrides:
#   RETRY_MAX      total attempts           (default: 3)
#   RETRY_BACKOFF  base seconds, linear     (default: 5)
#                  waits RETRY_BACKOFF*attempt between tries

: "${RETRY_MAX:=3}"
: "${RETRY_BACKOFF:=5}"

if [[ $# -eq 0 ]]; then
  echo "usage: retry.sh <cmd> [args...]" >&2
  exit 2
fi

attempt=1
while : ; do
  rc=0
  out=$("$@" 2>&1) || rc=$?
  if (( rc == 0 )); then
    printf '%s' "$out"
    exit 0
  fi
  echo "Retry attempt ${attempt}/${RETRY_MAX} failed (exit ${rc}): $*" >&2
  printf '%s\n' "$out" >&2
  if (( attempt >= RETRY_MAX )); then
    exit "$rc"
  fi
  sleep $(( RETRY_BACKOFF * attempt ))
  attempt=$(( attempt + 1 ))
done
