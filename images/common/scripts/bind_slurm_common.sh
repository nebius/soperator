#!/bin/bash

# The script is designed to perform bind-mounting of directories and files of
# the core Slurm packages from a container into a jail file system.

set -x # Print actual command before executing it
set -e # Exit immediately if any command returns a non-zero error code

usage() { echo "usage: ${0} -j <path_to_jail_dir> [-h]" >&2; exit 1; }

while getopts j:h flag
do
    case "${flag}" in
        j) jaildir=${OPTARG};;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "$jaildir" ]; then
    usage
fi

ALT_ARCH="$(uname -m)"
SLURM_LIB_PATH="usr/lib/${ALT_ARCH}-linux-gnu/slurm"

echo "🔧 Using ALT_ARCH = ${ALT_ARCH}"

pushd "${jaildir}"

    # slurm-smd
    # https://gist.github.com/asteny/58df92e594b0b27190fcedf4b5815762
    echo "Bind-mount slurm-smd package content from container to the jail"
    mkdir -p "${SLURM_LIB_PATH}"
    mount --bind "/${SLURM_LIB_PATH}" "${SLURM_LIB_PATH}"

    touch "usr/lib/${ALT_ARCH}-linux-gnu/libslurm.so.43.0.0"
    mount --bind "/usr/lib/${ALT_ARCH}-linux-gnu/libslurm.so.43.0.0" "usr/lib/${ALT_ARCH}-linux-gnu/libslurm.so.43.0.0"
    pushd "usr/lib/${ALT_ARCH}-linux-gnu"
         ln -sf libslurm.so.43.0.0 libslurm.so.43
         ln -sf libslurm.so.43.0.0 libslurm.so
    popd

    # slurm-smd-dev
    # https://gist.github.com/asteny/83575cc83563a2ac8336c1525768c3e6

    # slurm-smd-libnss-slurm
    # https://gist.github.com/asteny/b5c6b7df0320657fd1b21212c8f7ef45
    echo "Bind-mount slurm-smd-libnss-slurm package content from container to the jail"
    touch "usr/lib/${ALT_ARCH}-linux-gnu/libnss_slurm.so.2"
    mount --bind "/usr/lib/${ALT_ARCH}-linux-gnu/libnss_slurm.so.2" "usr/lib/${ALT_ARCH}-linux-gnu/libnss_slurm.so.2"

    # slurm-smd-client
    # https://gist.github.com/asteny/988e08fbe978e1c6ba20e4aa2d87f114
    echo "Bind-mount slurm-smd-client package content from container to the jail"
    SLURM_BINARIES=(
        sacct salloc sbatch scancel scrontab sdiag sinfo squeue srun sstat
        sacctmgr sattach sbcast scontrol scrun sh5util sprio sreport sshare strigger
    )

    for binary in "${SLURM_BINARIES[@]}"; do
        touch "usr/bin/$binary"
        mount --bind "/usr/bin/$binary" "usr/bin/$binary"
    done

    # bash completions
    touch usr/share/bash-completion/completions/slurm_completion.sh
    mount --bind /usr/share/bash-completion/completions/slurm_completion.sh usr/share/bash-completion/completions/slurm_completion.sh
    pushd usr/share/bash-completion/completions
        SLURM_BASH_COMPLETION=(
            sacct salloc sbatch scancel scrontab sinfo slurmrestd squeue srun sstat
            sacctmgr sattach sbcast scontrol sdiag sprio sreport sshare strigger
        )
        for completion in "${SLURM_BASH_COMPLETION[@]}"; do
            ln -sf slurm_completion.sh "$completion"
        done
    popd

popd
