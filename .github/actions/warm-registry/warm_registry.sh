#!/usr/bin/env bash
set -euo pipefail

IMAGE=${1:?Usage: warm_registry.sh cr.nebius.cloud/namespace/image:tag}
case "$IMAGE" in
  cr.nebius.cloud/*:*) ;;
  *) echo "::warning::Skipping registry warming for $IMAGE"; exit 0 ;;
esac
REPOSITORY=${IMAGE#cr.nebius.cloud/}
TAG=${REPOSITORY##*:}
REPOSITORY=${REPOSITORY%:*}

for REGION in eu-west2 eu-north1; do
  REGISTRY="cr.${REGION}.nebius.cloud"
  if ! TOKEN=$(printf '%s\n' "$REGISTRY" |
    nebius --no-browser --no-check-update --auth-timeout 10s --timeout 10s --retries 1 \
      registry docker-credential get 2>/dev/null |
    jq -er '.Secret | select(type == "string" and length > 0)'); then
    echo "::warning::Cannot authenticate for warming $REGISTRY/$REPOSITORY:$TAG"
    continue
  fi

  # Pass the credential through stdin, not curl's command-line arguments.
  if STATUS=$(printf 'Authorization: Bearer %s\n' "$TOKEN" |
    curl --head --silent --show-error --fail \
      --connect-timeout 5 --max-time 15 --output /dev/null --write-out '%{http_code}' \
      --header @- --header 'Accept: */*' \
      "https://$REGISTRY/v2/$REPOSITORY/manifests/$TAG") && [[ "$STATUS" == 200 ]]; then
    echo "Warming requested: $REGISTRY/$REPOSITORY:$TAG"
  else
    echo "::warning::Warming failed: $REGISTRY/$REPOSITORY:$TAG (HTTP $STATUS)"
  fi
done
