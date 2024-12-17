#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

echo "Starting slurmd entrypoint script"
if [ -n "${CGROUP_V2}" ]; then
    CGROUP_PATH=$(cat /proc/self/cgroup | sed 's/^0:://')

    if [ -n "${CGROUP_PATH}" ]; then
        echo "cgroup v2 detected, creating cgroup for ${CGROUP_PATH}"
        mkdir -p /sys/fs/cgroup/${CGROUP_PATH}/../system.slice
    else
        echo "cgroup v2 detected, but cgroup path is empty"
        exit 1
    fi
fi

echo "Link users from jail"
ln -s /mnt/jail/etc/passwd /etc/passwd
ln -s /mnt/jail/etc/group /etc/group
ln -s /mnt/jail/etc/shadow /etc/shadow
ln -s /mnt/jail/etc/gshadow /etc/gshadow
chown -h 0:42 /etc/{shadow,gshadow}

echo "Link home from jail because slurmd uses it"
ln -s /mnt/jail/home /home

echo "Bind-mount slurm configs from K8S config map"
for file in /mnt/slurm-configs/*; do
    filename=$(basename "$file")
    touch "/etc/slurm/$filename" && mount --bind "$file" "/etc/slurm/$filename"
done

echo "Make ulimits as big as possible"
set_ulimit() {
    local limit_option=$1
    local limit_value=$2
    ulimit $limit_option $limit_value || { echo "ulimit $limit_option: exit code: $?"; }
}
set_ulimit -HSR unlimited  # (-R) Max real-time non-blocking time
set_ulimit -HSc unlimited  # (-c) Max core file size
set_ulimit -HSd unlimited  # (-d) Max "data" segment size
set_ulimit -HSe unlimited  # (-e) Max scheduling priority
set_ulimit -HSf unlimited  # (-f) Max file size
set_ulimit -HSi unlimited  # (-i) Max number of pending signals
set_ulimit -HSl unlimited  # (-l) Max locked memory size (is necessary for Infiniband RDMA to work)
set_ulimit -HSm unlimited  # (-m) Max physical memory usage
set_ulimit -HSn 1048576    # (-n) Max number of open files
# READ-ONLY                # (-p) Max pipe size
set_ulimit -HSq unlimited  # (-q) Max POSIX message queue size
set_ulimit -HSr unlimited  # (-r) Max real-time priority
set_ulimit -HSs unlimited  # (-s) Max stack size
set_ulimit -HSt unlimited  # (-t) Max CPU time
set_ulimit -HSu unlimited  # (-u) Max number of user processes
set_ulimit -HSv unlimited  # (-v) Max virtual memory size
set_ulimit -HSx unlimited  # (-x) Max number of file locks

echo "Apply sysctl limits from /etc/sysctl.conf"
sysctl -p

echo "Update linker cache"
ldconfig

echo "Complement jail rootfs"
/opt/bin/slurm/complement_jail.sh -j /mnt/jail -u /mnt/jail.upper -w

echo "Create privilege separation directory /var/run/sshd"
mkdir -p /var/run/sshd

# TODO: Since 1.29 kubernetes supports native sidecar containers. We can remove it in feature releases
echo "Waiting until munge is started"
while [ ! -S "/run/munge/munge.socket.2" ]; do sleep 2; done

GRES=""
if [ "$SLURM_CLUSTER_TYPE" = "gpu" ]; then
    echo "Slurm cluster type is - $SLURM_CLUSTER_TYPE Detect available GPUs"
    # The following command converts the nvidia-smi output into the Gres GPU string expected by Slurm.
    # For example, if "nvidia-smi --list-gpus" shows this:
    #   GPU 0: NVIDIA A100-SXM4-80GB (UUID: <...>)
    #   GPU 1: NVIDIA A100-SXM4-80GB (UUID: <...>)
    #   GPU 2: NVIDIA V100-SXM4-16GB (UUID: <...>)
    # the GRES variable will be equal to "gpu:nvidia_a100-sxm4-80gb:2,gpu:nvidia_v100-sxm2-16gb:1".
    # See Slurm docs: https://slurm.schedmd.com/gres.html#AutoDetect
    export GRES="$(nvidia-smi --query-gpu=name --format=csv,noheader | sed -e 's/ /_/g' -e 's/.*/\L&/' | sort | uniq -c | awk '{print "gpu:" $2 ":" $1}' | paste -sd ',' -)"
    
    echo "Detected GRES is $GRES"

    echo "Create NVML symlink with the name expected by Slurm"
    pushd /usr/lib/x86_64-linux-gnu
        ln -s libnvidia-ml.so.1 libnvidia-ml.so
    popd
else
    echo "Skipping GPU detection"
fi

# Hack with logs: multilog will write log in stdout and in log file, and rotate log file
echo "Start supervisord daemon"
/usr/bin/supervisord
