#!/bin/bash

set -euo pipefail

: "${NEBIUS_CLI_CONFIG:?NEBIUS_CLI_CONFIG is required}"
: "${NEBIUS_PRIVATE_KEY:?NEBIUS_PRIVATE_KEY is required}"

config_dir="${HOME}/.nebius"
config_path="${config_dir}/config.yaml"

fail() {
  echo "::error::$*" >&2
  exit 1
}

set_private_key() {
  local profile="$1"
  local private_key="$2"

  [[ -n "$private_key" ]] || return

  NEBIUS_PROFILE_NAME="$profile" \
    yq -e '.profiles[strenv(NEBIUS_PROFILE_NAME)] != null' "$config_path" >/dev/null ||
    fail "Nebius profile '$profile' is missing from NEBIUS_CLI_CONFIG"

  NEBIUS_PROFILE_NAME="$profile" NEBIUS_PROFILE_PRIVATE_KEY="$private_key" \
    yq -i '.profiles[strenv(NEBIUS_PROFILE_NAME)].private-key = strenv(NEBIUS_PROFILE_PRIVATE_KEY)' \
      "$config_path"
}

validate_profile() {
  local profile="$1"

  NEBIUS_PROFILE_NAME="$profile" \
    yq -e '.profiles[strenv(NEBIUS_PROFILE_NAME)].private-key | length > 0' \
      "$config_path" >/dev/null ||
    fail "Nebius profile '$profile' is not configured with a private key"
}

umask 077
mkdir -p "$config_dir"
printf '%s\n' "$NEBIUS_CLI_CONFIG" > "$config_path"

set_private_key default "$NEBIUS_PRIVATE_KEY"
set_private_key testing "${NEBIUS_TESTING_PRIVATE_KEY:-}"
set_private_key soperator-telemetry "${NEBIUS_TELEMETRY_PRIVATE_KEY:-}"

selected_profile="${NEBIUS_PROFILE:-$(yq -er '.default' "$config_path")}"
validate_profile "$selected_profile"

if [[ -n "${E2E_CONFIG:-}" ]]; then
  while IFS= read -r profile; do
    validate_profile "$profile"
  done < <(yq -r '.profiles[].nebius_profile // empty' <<<"$E2E_CONFIG" | sort -u)
fi
