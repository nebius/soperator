#!/bin/sh

[ -n "${BASH_VERSION:-}" ] || return 0
case $- in
    *i*) ;;
    *) return 0 ;;
esac

[ -r /usr/lib/soperator/login_warning.sh ] || return 0
. /usr/lib/soperator/login_warning.sh --source

[ -d /run/soperator-login-state ] || return 0

# Exported prompt commands may reach shells that have not loaded this file.
_soperator_login_warning_command='_soperator_login_warning_status=$?
if declare -F _soperator_warn_outdated_login >/dev/null; then
    _soperator_warn_outdated_login "$_soperator_login_warning_status"
else
    (exit "$_soperator_login_warning_status")
fi'
if [[ $(declare -p PROMPT_COMMAND 2>/dev/null) == 'declare -a'* ]]; then
    _soperator_login_warning_registered=false
    for _soperator_login_warning_entry in "${PROMPT_COMMAND[@]}"; do
        if [[ "$_soperator_login_warning_entry" == "$_soperator_login_warning_command" ]]; then
            _soperator_login_warning_registered=true
            break
        fi
    done
    if [ "$_soperator_login_warning_registered" != true ]; then
        PROMPT_COMMAND=("$_soperator_login_warning_command" "${PROMPT_COMMAND[@]}")
    fi
else
    case "${PROMPT_COMMAND:-}" in
        *"$_soperator_login_warning_command"*) ;;
        *) PROMPT_COMMAND="$_soperator_login_warning_command${PROMPT_COMMAND:+; $PROMPT_COMMAND}" ;;
    esac
fi
unset _soperator_login_warning_command _soperator_login_warning_registered _soperator_login_warning_entry
