#!/bin/sh
set -eu

: "${TSA_TOKEN_PATH:=/mnt/imds/tsa-token}"
: "${TSA_TOKEN_EXPIRY_PATH:=${TSA_TOKEN_PATH}.expires_at}"
: "${IMDS_BASE_URL:=http://169.254.169.254}"
: "${TSA_TOKEN_REFRESH_SAFETY_MARGIN_SECONDS:=900}"

mkdir -p "$(dirname "${TSA_TOKEN_PATH}")" "$(dirname "${TSA_TOKEN_EXPIRY_PATH}")"

json_string_field() {
  field="$1"
  sed -n "s/.*\"${field}\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" | head -n 1
}

timestamp_to_epoch() {
  timestamp="$1"
  case "${timestamp}" in
    *.*Z) timestamp="${timestamp%%.*}Z" ;;
  esac
  if date -u -d "${timestamp}" +%s >/tmp/tsa-token-date 2>/dev/null; then
    cat /tmp/tsa-token-date
    return 0
  fi
  date -u -D "%Y-%m-%dT%H:%M:%SZ" -d "${timestamp}" +%s
}

fetch_and_save_token() {
  response="$(curl -fsSL -H "Metadata: true" "${IMDS_BASE_URL}/v1/iam/tsa/token")"
  access_token="$(printf '%s\n' "${response}" | json_string_field "access_token")"
  expires_at="$(printf '%s\n' "${response}" | json_string_field "expires_at")"

  if [ -z "${access_token}" ] || [ -z "${expires_at}" ]; then
    echo "IMDS TSA token response did not include access_token or expires_at" >&2
    return 1
  fi

  token_tmp="${TSA_TOKEN_PATH}.tmp"
  expiry_tmp="${TSA_TOKEN_EXPIRY_PATH}.tmp"
  printf '%s' "${access_token}" > "${token_tmp}"
  printf '%s\n' "${expires_at}" > "${expiry_tmp}"
  chmod 0444 "${token_tmp}" "${expiry_tmp}"
  mv "${token_tmp}" "${TSA_TOKEN_PATH}"
  mv "${expiry_tmp}" "${TSA_TOKEN_EXPIRY_PATH}"

  echo "refreshed tsa-token, expires at ${expires_at}"
}

seconds_until_refresh() {
  expires_epoch="$(timestamp_to_epoch "$(cat "${TSA_TOKEN_EXPIRY_PATH}")")"
  now_epoch="$(date -u +%s)"
  sleep_seconds=$((expires_epoch - now_epoch - TSA_TOKEN_REFRESH_SAFETY_MARGIN_SECONDS))

  if [ "${sleep_seconds}" -lt 1 ]; then
    echo 1
    return 0
  fi

  echo "${sleep_seconds}"
}

while true; do
  fetch_and_save_token
  sleep "$(seconds_until_refresh)"
done
