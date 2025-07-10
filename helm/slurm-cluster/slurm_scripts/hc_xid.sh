#!/bin/bash

set -eox pipefail

echo "[$(date)] Check for critical NVIDIA Xid / Sxid errors"

# The list of critical Xid errors (GPU)
xids=(
    46
    48
    62
    63
    64
    79
    110
    119
    120
    136
    140
    143
    155
    156
    158
)
xid_pattern=$(IFS=\|; echo "${xids[*]}")

# The list of critical Sxid errors (NVSwitch)
sxids=(
    10003
    10006
    10007
    10008
    11001
    11009
    11013
    11017
    11018
    11019
    11020
    11028
    11029
    11030
    12001
    12002
    12020
    12022
    12024
    12025
    12026
    12027
    12030
    12031
    12032
    12035
    12037
    12039
    12041
    12043
    12045
    12047
    12048
    14001
    14002
    14008
    14010
    14017
    14018
    15001
    15009
    15010
    15012
    15013
    15015
    15017
    15019
    18020
    19004
    19013
    19014
    19015
    19021
    19036
    19037
    19046
    19047
    19048
    19053
    19054
    19056
    19058
    19060
    19061
    19063
    19064
    19066
    19067
    19069
    19070
    19080
    19083
    19084
    20004
    20005
    20006
    20007
    20034
    20036
    20037
    20038
    22003
    22011
    22012
    23001
    23002
    23003
    23004
    23005
    23006
    23007
    23008
    23009
    23010
    23011
    23012
    23013
    23014
    23015
    23016
    23017
    24004
    24005
    24007
    24008
    24009
    24010
    25008
    25009
    25010
    26002
    26003
    26004
    26006
    26007
    29002
    29004
    30002
    30004
)
sxid_pattern=$(IFS=\|; echo "${sxids[*]}")

echo "Grep kernel log for critical Xids"
if lines=$(dmesg -T | grep -E "NVRM:.*Xid.*\b(${xid_pattern})\b" || echo ''); then
    if [[ -n "$lines" ]]; then
        echo "Critical Xid is found in dmesg"

        codes=()
        for xid in "${xids[@]}"; do
            if grep -q "\b${xid}\b" <<<"$lines"; then
                codes+=("$xid")
            fi
        done

        codes_str=$(IFS=,; echo "${codes[*]}")
        # Return failure details
        echo "xid ${codes_str}" >&3
        exit 1
    fi
fi

echo "Grep kernel log for critical Sxids"
if lines=$(dmesg -T | grep -E "NVRM:.*Sxid.*\b(${sxid_pattern})\b" || echo ''); then
    if [[ -n "$lines" ]]; then
        echo "Critical Sxid is found in dmesg"

        codes=()
        for sxid in "${sxids[@]}"; do
            if grep -q "\b${sxid}\b" <<<"$lines"; then
                codes+=("$sxid")
            fi
        done

        codes_str=$(IFS=,; echo "${codes[*]}")
        # Return failure details
        echo "sxid ${codes_str}" >&3
        exit 1
    fi
fi

exit 0
