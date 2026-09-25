#!/bin/bash
set -euo pipefail

# Slurm passes the configured global timeout through the PowerAction environment.
# Partition-specific shorter timeouts remain enforced independently by Slurm.
action="$1"
nodes="$2"
case "$action" in
    resume|suspend) ;;
    *) echo "Unknown power action: $action" >&2; exit 1 ;;
esac
power_manager=(/opt/soperator/bin/power-manager "$action" --nodes "$nodes" \
    --rest-config-qps "${POWER_MANAGER_REST_CONFIG_QPS:-5}" \
    --rest-config-burst "${POWER_MANAGER_REST_CONFIG_BURST:-10}")
timeout_seconds=${POWER_MANAGER_TIMEOUT:-}
if [[ "$timeout_seconds" =~ ^[1-9][0-9]*$ ]] && (( ${#timeout_seconds} <= 10 && timeout_seconds <= 2147483647 )); then
    if (( timeout_seconds > 5 )); then
        exec "${power_manager[@]}" --wait --timeout "$(( timeout_seconds - 5 ))s"
    fi
    exec "${power_manager[@]}" --timeout "${timeout_seconds}s"
else
    echo "Expected a positive POWER_MANAGER_TIMEOUT from Slurm, got: $timeout_seconds" >&2
fi
# Direct calls without the generated PowerAction still apply the desired state.
echo "Applying $action without a readiness wait" >&2
exec "${power_manager[@]}"
