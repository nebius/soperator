#!/bin/bash

set -euo pipefail

oom_score_adj_file=/run/soperator-default-oom-score-adj
if [ ! -s "${oom_score_adj_file}" ]; then
    echo "Login SSHD: default OOM score adjustment is unavailable" >&2
    exit 1
fi

# Supervisord is protected with -1000. Restore the Kubernetes-assigned value
# before starting OpenSSH so the master can protect itself while session
# children return to the original value through OpenSSH's normal OOM handling.
cat "${oom_score_adj_file}" > /proc/self/oom_score_adj

exec /usr/sbin/sshd -D -e -f /run/soperator-sshd_config
