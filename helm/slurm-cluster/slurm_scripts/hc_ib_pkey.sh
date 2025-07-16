#!/bin/bash

set -eox pipefail

echo "[$(date)] Check if corresponding PKeys for each IB device and each port are equal"

expected_ports=8

# Traverse all PKeys and get ones that have not the same value for all $expected_ports IB ports:
#      <number_of_ports> <pkey_number>:<pkey_value>
# Example:
#      7 13:0x1234
#      1 13:0x0000
#      5 98:0x4321
#      3 98:0x1337
#      ...
inconsistent_pkeys=$(find -L /sys/class/infiniband -maxdepth 5 -path '*/ports/*/pkeys/*' -print0 2>/dev/null \
    | xargs -0 grep -H . \
    | sed -E 's|.*/([^/]+):|\1:|' \
    | sort | uniq -c \
    | grep -vE "^[[:space:]]*${expected_ports} " || echo '')

if [[ -n "${inconsistent_pkeys}" ]]; then
    echo "Found IB ports that have inconsistent PKey value for the same PKey file"

    first_pkey_file=$(printf '%s\n' "$inconsistent_pkeys" | head -n1 | awk '{split($2, a, ":"); print a[1]}')
    # Return failure details
    echo "inconsistent PKey #${first_pkey_file}" >&3
    exit 1
fi

exit 0
