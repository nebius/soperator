#!/bin/sh
set -eu

# Maintains the o11y bearer token in a Kubernetes Secret so that consumers
# (vmagent, otel collectors) can reference it via bearerTokenSecret. The token
# is obtained from one of two sources:
#   imds - fetched from the instance metadata service (IMDS_BASE_URL may be a proxy)
#   file - read from a mounted file (e.g. a host-provided token file)

: "${TOKEN_SOURCE:=imds}"
: "${SECRET_NAME:?SECRET_NAME is required}"
: "${SECRET_KEY:=accessToken}"
: "${SECRET_NAMESPACES:?SECRET_NAMESPACES is required}"
: "${IMDS_BASE_URL:=http://169.254.169.254}"
: "${TOKEN_FILE:=/o11ytoken/tsa-token}"
: "${TOKEN_EXPIRY_FILE:=}"
: "${REFRESH_SAFETY_MARGIN_SECONDS:=900}"
: "${POLL_INTERVAL_SECONDS:=300}"

SA_DIR=/var/run/secrets/kubernetes.io/serviceaccount
APISERVER="https://kubernetes.default.svc"
CACERT="${SA_DIR}/ca.crt"

json_string_field() {
  field="$1"
  sed -n "s/.*\"${field}\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" | head -n 1
}

timestamp_to_epoch() {
  timestamp="$1"
  case "${timestamp}" in
    *.*Z) timestamp="${timestamp%%.*}Z" ;;
  esac
  if date -u -d "${timestamp}" +%s 2>/dev/null; then
    return 0
  fi
  date -u -D "%Y-%m-%dT%H:%M:%SZ" -d "${timestamp}" +%s
}

# Sets ACCESS_TOKEN and EXPIRES_AT (EXPIRES_AT may be empty)
obtain_token() {
  case "${TOKEN_SOURCE}" in
    imds)
      response="$(curl -fsSL -H "Metadata: true" "${IMDS_BASE_URL}/v1/iam/tsa/token")"
      ACCESS_TOKEN="$(printf '%s\n' "${response}" | json_string_field "access_token")"
      EXPIRES_AT="$(printf '%s\n' "${response}" | json_string_field "expires_at")"
      ;;
    file)
      ACCESS_TOKEN="$(cat "${TOKEN_FILE}")"
      if [ -n "${TOKEN_EXPIRY_FILE}" ] && [ -f "${TOKEN_EXPIRY_FILE}" ]; then
        EXPIRES_AT="$(cat "${TOKEN_EXPIRY_FILE}")"
      else
        EXPIRES_AT=""
      fi
      ;;
    *)
      echo "unsupported TOKEN_SOURCE=${TOKEN_SOURCE} (want: imds|file)" >&2
      return 1
      ;;
  esac

  if [ -z "${ACCESS_TOKEN}" ]; then
    echo "obtained empty token from source=${TOKEN_SOURCE}" >&2
    return 1
  fi
}

upsert_secret() {
  token_b64="$(printf '%s' "${ACCESS_TOKEN}" | base64 | tr -d '\n')"
  k8s_token="$(cat "${SA_DIR}/token")"
  patch="{\"data\":{\"${SECRET_KEY}\":\"${token_b64}\"}}"
  create="{\"apiVersion\":\"v1\",\"kind\":\"Secret\",\"metadata\":{\"name\":\"${SECRET_NAME}\"},\"data\":{\"${SECRET_KEY}\":\"${token_b64}\"}}"

  for ns in ${SECRET_NAMESPACES}; do
    code="$(curl -sS -o /tmp/api.out -w '%{http_code}' \
      --cacert "${CACERT}" -H "Authorization: Bearer ${k8s_token}" \
      -H 'Content-Type: application/merge-patch+json' \
      -X PATCH "${APISERVER}/api/v1/namespaces/${ns}/secrets/${SECRET_NAME}" -d "${patch}")"
    if [ "${code}" = "404" ]; then
      code="$(curl -sS -o /tmp/api.out -w '%{http_code}' \
        --cacert "${CACERT}" -H "Authorization: Bearer ${k8s_token}" \
        -H 'Content-Type: application/json' \
        -X POST "${APISERVER}/api/v1/namespaces/${ns}/secrets" -d "${create}")"
    fi
    case "${code}" in
      2*) echo "synced secret ${ns}/${SECRET_NAME}" ;;
      *) echo "sync secret ${ns}/${SECRET_NAME} failed: HTTP ${code}: $(cat /tmp/api.out)" >&2; return 1 ;;
    esac
  done
}

seconds_until_refresh() {
  if [ -z "${EXPIRES_AT}" ]; then
    echo "${POLL_INTERVAL_SECONDS}"
    return 0
  fi
  expires_epoch="$(timestamp_to_epoch "${EXPIRES_AT}")"
  now_epoch="$(date -u +%s)"
  sleep_seconds=$((expires_epoch - now_epoch - REFRESH_SAFETY_MARGIN_SECONDS))
  if [ "${sleep_seconds}" -lt 1 ]; then
    echo 1
  else
    echo "${sleep_seconds}"
  fi
}

while true; do
  obtain_token
  upsert_secret
  sleep "$(seconds_until_refresh)"
done
