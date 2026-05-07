#!/bin/bash
# Updates soperator Grafana dashboards in a Kubernetes cluster by replacing ConfigMaps.
# Usage: ./update-dashboards.sh <k8s-context-name> [dashboard-file]
#
# Reads JSONs from ../dashboards (relative to this script) and creates/replaces
# ConfigMaps named "soperator-<dashboard-name>" with a "grafana_dashboard=1" label
# in the "monitoring-system" namespace, matching what helm/soperator-monitoring-dashboards
# would produce. Use this for fast iteration on dashboards without running `helm upgrade`.
#
# If [dashboard-file] is given (e.g. "cluster_health.json" or just "cluster_health"),
# only that single dashboard is updated. Otherwise all dashboards in ../dashboards are
# processed.

set -euo pipefail

NAMESPACE="monitoring-system"

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
    echo "Error: please provide a Kubernetes context name (and optionally a dashboard file)"
    echo "Usage: $0 <k8s-context-name/> [dashboard-file]"
    exit 1
fi

CONTEXT="$1"
SINGLE_DASHBOARD="${2:-}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DASHBOARD_DIR="${SCRIPT_DIR}/../dashboards"

if [ ! -d "${DASHBOARD_DIR}" ]; then
    echo "Error: dashboard directory not found at ${DASHBOARD_DIR}"
    exit 1
fi

DASHBOARDS=()
if [ -n "${SINGLE_DASHBOARD}" ]; then
    candidate="${SINGLE_DASHBOARD}"
    [[ "${candidate}" != *.json ]] && candidate="${candidate}.json"
    if [ ! -f "${DASHBOARD_DIR}/${candidate}" ]; then
        echo "Error: dashboard file not found: ${DASHBOARD_DIR}/${candidate}"
        exit 1
    fi
    DASHBOARDS=("${candidate}")
else
    shopt -s nullglob
    for f in "${DASHBOARD_DIR}"/*.json; do
        DASHBOARDS+=("$(basename "$f")")
    done
    shopt -u nullglob
fi

if [ ${#DASHBOARDS[@]} -eq 0 ]; then
    echo "Error: no JSON dashboard files found in ${DASHBOARD_DIR}"
    exit 1
fi

echo "Updating soperator Grafana dashboards..."
echo "Dashboard directory: ${DASHBOARD_DIR}"
echo "Namespace: ${NAMESPACE}"
echo "Found ${#DASHBOARDS[@]} dashboard file(s): ${DASHBOARDS[*]}"
echo

kubectl config use-context "${CONTEXT}"
echo

updated_count=0
skipped_count=0

for file in "${DASHBOARDS[@]}"; do
    # Match the chart's helper: filename without .json, with `_` -> `-`.
    dashboard_name="${file%.json}"
    dashboard_name="${dashboard_name//_/-}"
    configmap_name="soperator-${dashboard_name}"
    data_key="${dashboard_name}.json"

    local_hash=$(md5sum "${DASHBOARD_DIR}/${file}" | cut -d' ' -f1)

    current_configmap_hash=""
    if kubectl get configmap "${configmap_name}" -n "${NAMESPACE}" >/dev/null 2>&1; then
        current_configmap_hash=$(kubectl get configmap "${configmap_name}" -n "${NAMESPACE}" -o jsonpath="{.data['${data_key//./\\.}']}" 2>/dev/null | md5sum | cut -d' ' -f1)
    fi

    if [ "$local_hash" = "$current_configmap_hash" ] && [ -n "$current_configmap_hash" ]; then
        echo "✓ ${configmap_name} - no changes, skipping"
        skipped_count=$((skipped_count + 1))
        continue
    fi

    echo "↻ ${configmap_name} - updating..."

    kubectl delete configmap "${configmap_name}" -n "${NAMESPACE}" 2>/dev/null || true

    kubectl create configmap "${configmap_name}" -n "${NAMESPACE}" \
        --from-file="${data_key}=${DASHBOARD_DIR}/${file}"

    kubectl label configmap "${configmap_name}" -n "${NAMESPACE}" grafana_dashboard=1

    updated_count=$((updated_count + 1))
done

echo
if [ $updated_count -eq 0 ]; then
    echo "✅ All dashboards are up to date!"
else
    echo "✅ Updated $updated_count dashboard(s), skipped $skipped_count unchanged"
    echo "⏳ Waiting for Grafana to reload dashboards automatically..."
    for i in {30..1}; do
        printf "\r⏳ Wait for %2d seconds..." "$i"
        sleep 1
    done
    printf "\r✅ Done waiting! Dashboards should be reloaded.\n"
fi
