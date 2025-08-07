#!/bin/bash

set -eox

echo "[$(date)] Run Nebius GPU health-checker"

# PATH is required to be propagated inside health-checker
if [[ -z "${PATH:-}" ]]; then
    echo "PATH is not provided, skipping health-checker" >&2
    exit 0
fi
# health-checker library uses os.Environ and we need to explicitly export PATH
export PATH=$PATH

# Define platform for health-checker
platform="$CHECKS_PLATFORM_TAG"
echo "Platform: $platform"

# Define health-checker checks to run
case "$CHECKS_CONTEXT" in
  "prolog")
    checks="module,nvidia_smi,nvidia_smi_nvlink,nvidia_smi_topo,dmesg"
    ;;
  "epilog")
    checks="module,nvidia_smi,nvidia_smi_nvlink,nvidia_smi_topo,dcgmi_diag_r1,dmesg"
    ;;
  "hc_program")
    checks="module,nvidia_smi,nvidia_smi_nvlink,nvidia_smi_topo,dmesg"
    ;;
  *)
    echo "Unknown context: $CHECKS_CONTEXT" >&2
    exit 0
    ;;
esac

exit_code=0
details=$(health-checker run -e soperator -p "$platform" -f mk8s-txt -n "$checks" 2>&1) || exit_code=$?
if [[ $exit_code -eq 1 ]]; then
    echo "Health-checker failed with exit code 1."
    echo "$details"

    # Extract the name of the first failed check
    error_checks=$(echo "$details" | sed -n 's/.*S: FAIL \[\([^ ,'\''"]*\).*/\1/p' | head -n 1)
    echo "$error_checks" >&3
    exit 1
elif [[ $exit_code -eq 2 ]]; then
    echo "Health-checker reported with exit code 2." >&2
    echo "$details"
    exit 0
else
    echo "Health-checker passed or returned non-ERROR status."
    echo "$details"
    exit 0
fi
