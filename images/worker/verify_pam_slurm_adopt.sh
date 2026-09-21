#!/bin/bash

set -euo pipefail

if [[ "${SOPERATOR_PAM_SLURM_ADOPT_ENABLED:-false}" != "true" ]]; then
    exit 0
fi

sshd_config="${1:-/mnt/ssh-configs/sshd_config}"
sshd_pam_config="/etc/pam.d/sshd"
adopt_pam_config="/etc/pam.d/soperator-pam-slurm-adopt"

fail() {
    echo "pam_slurm_adopt preflight: $*" >&2
    exit 1
}

find_pam_module() {
    local module_name="$1"
    local module_path=""
    local module_root

    for module_root in /lib /lib64 /usr/lib /usr/lib64; do
        [[ -d "${module_root}" ]] || continue
        module_path=$(find "${module_root}" -type f -name "${module_name}" -print -quit)
        [[ -n "${module_path}" ]] && break
    done
    printf '%s' "${module_path}"
}

check_pam_module() {
    local module_name="$1"
    local module_path
    local linker_output

    module_path=$(find_pam_module "${module_name}")
    [[ -n "${module_path}" ]] || fail "${module_name} is not installed"
    if ! linker_output=$(ldd "${module_path}" 2>&1); then
        fail "unable to load ${module_path}: ${linker_output}"
    fi
    if grep -q 'not found' <<<"${linker_output}"; then
        fail "${module_path} has unresolved dependencies: ${linker_output}"
    fi
}

[[ -r "${adopt_pam_config}" ]] || fail "missing ${adopt_pam_config}"

adopt_rule_pattern='^[[:space:]]*-?account[[:space:]]+required[[:space:]]+pam_slurm_adopt\.so([[:space:]]|$)'
grep -Eq "${adopt_rule_pattern}" "${adopt_pam_config}" || \
    fail "pam_slurm_adopt is not a required account module"

last_account_rule=$(grep -E '^[[:space:]]*-?account[[:space:]]+' "${adopt_pam_config}" | tail -n 1)
[[ "${last_account_rule}" =~ pam_slurm_adopt\.so ]] || \
    fail "pam_slurm_adopt is not the last account rule in ${adopt_pam_config}"

for option in \
    action_no_jobs=deny \
    action_adopt_failure=deny \
    action_generic_failure=deny \
    disable_x11=1 \
    join_container=false; do
    grep -Eq "(^|[[:space:]])${option}([[:space:]]|$)" "${adopt_pam_config}" || \
        fail "missing required option ${option}"
done
grep -Eq '(^|[[:space:]])action_unknown=(newest|deny)([[:space:]]|$)' "${adopt_pam_config}" || \
    fail "action_unknown must be newest or deny"

check_pam_module pam_slurm_adopt.so
if grep -Eq '^[[:space:]]*account[[:space:]]+sufficient[[:space:]]+pam_listfile\.so([[:space:]]|$)' \
    "${adopt_pam_config}"; then
    while read -r listfile; do
        [[ -r "${listfile}" ]] || fail "missing pam_slurm_adopt exemption list ${listfile}"
    done < <(grep -Eo 'file=[^[:space:]]+' "${adopt_pam_config}" | cut -d= -f2-)
    check_pam_module pam_listfile.so
fi

include_pattern='^[[:space:]]*@include[[:space:]]+soperator-pam-slurm-adopt[[:space:]]*$'
grep -Eq "${include_pattern}" "${sshd_pam_config}" || \
    fail "${sshd_pam_config} does not include soperator-pam-slurm-adopt"
include_line=$(grep -nE "${include_pattern}" "${sshd_pam_config}" | tail -n 1 | cut -d: -f1)
if tail -n "+$((include_line + 1))" "${sshd_pam_config}" | \
    grep -Eq '^[[:space:]]*-?account[[:space:]]+'; then
    fail "soperator-pam-slurm-adopt is not the last account include in ${sshd_pam_config}"
fi

for pam_config in "${sshd_pam_config}" /etc/pam.d/common-session; do
    if [[ -r "${pam_config}" ]] && \
        grep -Eq '^[[:space:]]*-?session[[:space:]].*pam_systemd\.so([[:space:]]|$)' "${pam_config}"; then
        fail "pam_systemd is active in ${pam_config}"
    fi
done

effective_sshd_config=$(/usr/sbin/sshd -T -f "${sshd_config}")
grep -Eq '^usepam yes$' <<<"${effective_sshd_config}" || fail "sshd UsePAM is not enabled"
grep -Eq '^kbdinteractiveauthentication no$' <<<"${effective_sshd_config}" || \
    fail "sshd keyboard-interactive authentication is enabled"
if grep -Eq '^authenticationmethods .*keyboard-interactive' <<<"${effective_sshd_config}"; then
    fail "sshd AuthenticationMethods contains unsupported keyboard-interactive authentication"
fi

echo "pam_slurm_adopt preflight passed"
