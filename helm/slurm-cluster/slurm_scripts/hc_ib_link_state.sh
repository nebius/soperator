#!/bin/bash

set -eox pipefail

echo "[$(date)] Check if all IB ports have correct state, phys_state, and rate"

NEEDED=8
EXP_RATE='400 Gb/sec (4X NDR)'
EXP_STATE='4: ACTIVE'
EXP_PHYS='5: LinkUp'
ok=0

for n in 0 1 2 3 4 5 6 7; do
    card="mlx5_$n"
    base="/sys/class/infiniband/$card/ports/1"

    if [[ ! -d "$base" ]]; then
        echo "$base: missing (skipping)" >&2
        continue
    fi

    rate=$(cat "$base/rate" 2>/dev/null || echo "")
    state=$(cat "$base/state" 2>/dev/null || echo "")
    phys=$(cat "$base/phys_state" 2>/dev/null || echo "")

    fail=0
    [ "$rate"  = "$EXP_RATE"  ] || { echo "$base/rate: $rate"; fail=1; }
    [ "$state" = "$EXP_STATE" ] || { echo "$base/state: $state"; fail=1; }
    [ "$phys"  = "$EXP_PHYS"  ] || { echo "$base/phys_state: $phys"; fail=1; }
    [ $fail -eq 0 ] && ok=$((ok+1))
done

if [ $ok -eq $NEEDED ]; then
    echo "IB ports check: all $ok ports are healthy"
    exit 0
else
    echo "IB ports check: $ok of $NEEDED IB ports are healthy"
    # Return failure details
    echo "only $ok of $NEEDED IB ports are healthy" >&3
    exit 1
fi

exit 0
