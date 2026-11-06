#!/bin/bash

case $- in
    *i*) ;;
    *) return ;;
esac

_soperator_warn_outdated_login() {
    local command_status=$?
    local announced_at=
    local scheduled_at=

    # IDE terminals may have no SSH_TTY, and remote commands may have a PTY.
    # This hook is installed only in interactive shells; print to the terminal.
    if [[ -t 2 ]]; then
        { IFS= read -r announced_at < /run/soperator-login-state/announced-at; } 2>/dev/null || :
        { IFS= read -r scheduled_at < /run/soperator-login-state/scheduled-at; } 2>/dev/null || :
        if [[ -n "$announced_at" && -n "$scheduled_at" ]]; then
            if [[ "${_soperator_login_warning_schedule:-}" != "$scheduled_at" ]]; then
                local heading='Warning: This login host is outdated.'
                local restart_time="${scheduled_at/T/ }"
                restart_time="${restart_time/%Z/ UTC}"
                if [[ -n "${TERM:-}" && "$TERM" != dumb && -z "${NO_COLOR:-}" ]]; then
                    printf '\n\033[1;33m%s\033[0m\n' "$heading" >&2
                else
                    printf '\n%s\n' "$heading" >&2
                fi
                printf 'Scheduled restart: at or after %s.\n\n' "$restart_time" >&2
                printf 'The restart will end SSH connections, tmux sessions, and other\n' >&2
                printf 'processes on this host. Save your work before the scheduled restart.\n\n' >&2
                printf 'To request an early restart, an administrator can run:\n' >&2
                printf '  sudo soperator-login-restart\n' >&2
                printf 'The command asks for confirmation; restarting affects all users on this host.\n\n' >&2
                _soperator_login_warning_schedule="$scheduled_at"
            fi
        else
            unset _soperator_login_warning_schedule
        fi
    fi

    return "$command_status"
}

if [[ "${_soperator_login_warning_installed:-}" != true ]]; then
    # Keep existing prompt commands and the previous command's exit status.
    if [[ $(declare -p PROMPT_COMMAND 2>/dev/null) == 'declare -a'* ]]; then
        PROMPT_COMMAND=(_soperator_warn_outdated_login "${PROMPT_COMMAND[@]}")
    else
        PROMPT_COMMAND="_soperator_warn_outdated_login${PROMPT_COMMAND:+; $PROMPT_COMMAND}"
    fi
    _soperator_login_warning_installed=true
fi
