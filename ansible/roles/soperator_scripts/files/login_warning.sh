#!/bin/sh

_soperator_warn_outdated_login() {
    local command_status="${1:-$?}"
    local announced_at=
    local scheduled_at=

    if [ -t 2 ]; then
        { IFS= read -r announced_at < /run/soperator-login-state/announced-at; } 2>/dev/null || :
        { IFS= read -r scheduled_at < /run/soperator-login-state/scheduled-at; } 2>/dev/null || :
        if [ -n "$announced_at" ] && [ -n "$scheduled_at" ]; then
            if [ "${_soperator_login_warning_schedule:-}" != "$scheduled_at" ]; then
                local heading='Warning: This login node is outdated.'
                local restart_time
                restart_time=$(python3 -c '
from datetime import datetime
import re
import sys
timestamp = re.sub(r"\.\d+", "", sys.argv[1]).replace("Z", "+00:00")
print(datetime.fromisoformat(timestamp).astimezone().strftime("%Y-%m-%d %H:%M:%S %Z"))
' "$scheduled_at" 2>/dev/null) || restart_time="$scheduled_at"
                if [ -n "${TERM:-}" ] && [ "$TERM" != dumb ] && [ -z "${NO_COLOR:-}" ]; then
                    printf '\n\033[1;33m%s\033[0m\n' "$heading" >&2
                else
                    printf '\n%s\n' "$heading" >&2
                fi
                printf 'Scheduled restart: at or after %s.\n\n' "$restart_time" >&2
                printf 'The restart will end SSH connections, tmux sessions, and other\n' >&2
                printf 'processes on this node. Save your work before the scheduled restart.\n\n' >&2
                printf 'To request an early restart, an administrator can run:\n' >&2
                printf '  sudo soperator-login-restart\n' >&2
                printf 'The command asks for confirmation; restarting affects all users on this node.\n\n' >&2
                _soperator_login_warning_schedule="$scheduled_at"
            fi
        else
            unset _soperator_login_warning_schedule
        fi
    fi

    return "$command_status"
}

# Adapters can source this file or execute --check and retain the returned schedule.
if [ "${1:-}" = --check ]; then
    _soperator_login_warning_schedule="${2:-}"
    _soperator_warn_outdated_login
    printf '%s' "${_soperator_login_warning_schedule:-}"
fi
