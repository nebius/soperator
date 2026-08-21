#!/bin/bash

set -euo pipefail

cgroup_parent_file=/run/soperator-docker-cgroup-parent
if [ ! -s "${cgroup_parent_file}" ]; then
    echo "Login Docker: cgroup parent is unavailable" >&2
    exit 1
fi

cgroup_parent=$(cat "${cgroup_parent_file}")

oom_score_adj_file=/run/soperator-default-oom-score-adj
if [ ! -s "${oom_score_adj_file}" ]; then
    echo "Login Docker: default OOM score adjustment is unavailable" >&2
    exit 1
fi

# Do not inherit supervisord's -1000 protection. Restore Kubernetes' initial
# adjustment first, then prefer killing/restarting dockerd over SSH services
# under container-level memory pressure. The second write is best-effort: if a
# platform does not allow lowering a high initial value to 500, leaving dockerd
# more killable is safe.
cat "${oom_score_adj_file}" > /proc/self/oom_score_adj
echo 500 > /proc/self/oom_score_adj 2>/dev/null || true

exec /usr/bin/dockerd --cgroup-parent="${cgroup_parent}"
