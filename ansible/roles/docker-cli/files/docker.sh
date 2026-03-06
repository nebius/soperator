#!/bin/bash

BASE_DIR="/mnt/jail"  # Base directory for chroot

# get_docker_cgroup_parent returns the path of the dedicated Docker cgroup that
# was created once at pod startup by supervisord_entrypoint.sh.
#
# All Docker containers (across all Slurm jobs) share this single cgroup.
# Its memory.max is set to REAL_MEMORY, so containers are OOM-killed
# at the Docker-cgroup level before the pod limit is reached, keeping slurmd stable.
get_docker_cgroup_parent() {
    local cgroup_file="/run/docker-cgroup-parent"
    [[ ! -f "$cgroup_file" ]] && return
    local cgroup_path
    cgroup_path=$(cat "$cgroup_file")
    [[ -z "$cgroup_path" ]] && return
    [[ -d "/sys/fs/cgroup/${cgroup_path}" ]] && echo "$cgroup_path"
}

# Pre-scan original arguments to find the Docker subcommand and whether
# --cgroup-parent has already been explicitly provided by the caller.
DOCKER_SUBCOMMAND=""
CGROUP_PARENT_SPECIFIED=false

for arg in "$@"; do
    if [[ -z "$DOCKER_SUBCOMMAND" ]] && [[ "$arg" != -* ]]; then
        DOCKER_SUBCOMMAND="$arg"
    fi
    if [[ "$arg" == "--cgroup-parent" ]] || [[ "$arg" == --cgroup-parent=* ]]; then
        CGROUP_PARENT_SPECIFIED=true
    fi
done

# Resolve the Docker cgroup parent once, before rebuilding arguments.
INJECT_CGROUP_PARENT=""
if [[ ("$DOCKER_SUBCOMMAND" == "run" || "$DOCKER_SUBCOMMAND" == "create") \
        && "$CGROUP_PARENT_SPECIFIED" == "false" ]]; then
    INJECT_CGROUP_PARENT=$(get_docker_cgroup_parent)
fi

ADJUSTED_ARGS=()
SKIP_NEXT_ARG=false

for ((i = 1; i <= $#; i++)); do
  arg="${!i}"

  if [[ "$SKIP_NEXT_ARG" == true ]]; then
    # Process the value for the previous space-separated argument
    SKIP_NEXT_ARG=false
    if [[ "${PREV_ARG}" == "-v" ]]; then
      HOST_PATH="${arg%%:*}"
      REST="${arg#*:}"
      # Only prepend BASE_DIR if the path is absolute
      if [[ "${HOST_PATH}" == /* ]]; then
        REAL_HOST_PATH="${BASE_DIR}${HOST_PATH}"
      else
        REAL_HOST_PATH="${HOST_PATH}"
      fi
      ADJUSTED_ARGS+=("${PREV_ARG}" "${REAL_HOST_PATH}:${REST}")
    elif [[ "${PREV_ARG}" == "--volume" ]]; then
      HOST_PATH="${arg%%:*}"
      REST="${arg#*:}"
      # Only prepend BASE_DIR if the path is absolute
      if [[ "${HOST_PATH}" == /* ]]; then
        REAL_HOST_PATH="${BASE_DIR}${HOST_PATH}"
      else
        REAL_HOST_PATH="${HOST_PATH}"
      fi
      ADJUSTED_ARGS+=("${PREV_ARG}" "${REAL_HOST_PATH}:${REST}")
    elif [[ "${PREV_ARG}" == "--mount" ]]; then
      IFS=',' read -ra MOUNT_PARAMS <<< "$arg"
      NEW_PARAMS=()
      for param in "${MOUNT_PARAMS[@]}"; do
        if [[ "$param" == source=* ]]; then
          HOST_PATH="${param#source=}"
          # Only prepend BASE_DIR if the path is absolute
          if [[ "${HOST_PATH}" == /* ]]; then
            REAL_HOST_PATH="${BASE_DIR}${HOST_PATH}"
          else
            REAL_HOST_PATH="${HOST_PATH}"
          fi
          NEW_PARAMS+=("source=${REAL_HOST_PATH}")
        else
          NEW_PARAMS+=("$param")
        fi
      done
      ADJUSTED_ARGS+=("${PREV_ARG}" "$(IFS=,; echo "${NEW_PARAMS[*]}")")
    fi
    continue
  fi

  if [[ "$arg" == -v ]]; then
    # Handle space-separated `-v value`
    PREV_ARG="-v"
    SKIP_NEXT_ARG=true
  elif [[ "$arg" == -v* ]]; then
    # Handle short form `-v=value`
    MOUNT_SPEC="${arg#-v}"
    HOST_PATH="${MOUNT_SPEC%%:*}"
    REST="${MOUNT_SPEC#*:}"
    # Only prepend BASE_DIR if the path is absolute
    if [[ "${HOST_PATH}" == /* ]]; then
      REAL_HOST_PATH="${BASE_DIR}${HOST_PATH}"
    else
      REAL_HOST_PATH="${HOST_PATH}"
    fi
    ADJUSTED_ARGS+=("-v" "${REAL_HOST_PATH}:${REST}")
  elif [[ "$arg" == --volume=* ]]; then
    # Handle long form `--volume=value`
    MOUNT_SPEC="${arg#--volume=}"
    HOST_PATH="${MOUNT_SPEC%%:*}"
    REST="${MOUNT_SPEC#*:}"
    # Only prepend BASE_DIR if the path is absolute
    if [[ "${HOST_PATH}" == /* ]]; then
      REAL_HOST_PATH="${BASE_DIR}${HOST_PATH}"
    else
      REAL_HOST_PATH="${HOST_PATH}"
    fi
    ADJUSTED_ARGS+=("--volume=${REAL_HOST_PATH}:${REST}")
  elif [[ "$arg" == --volume ]]; then
    # Handle space-separated `--volume value`
    PREV_ARG="--volume"
    SKIP_NEXT_ARG=true
  elif [[ "$arg" == --mount=* ]]; then
    # Handle long form `--mount=value`
    MOUNT_SPEC="${arg#--mount=}"
    IFS=',' read -ra MOUNT_PARAMS <<< "$MOUNT_SPEC"
    NEW_PARAMS=()
    for param in "${MOUNT_PARAMS[@]}"; do
      if [[ "$param" == source=* ]]; then
        HOST_PATH="${param#source=}"
        # Only prepend BASE_DIR if the path is absolute
        if [[ "${HOST_PATH}" == /* ]]; then
          REAL_HOST_PATH="${BASE_DIR}${HOST_PATH}"
        else
          REAL_HOST_PATH="${HOST_PATH}"
        fi
        NEW_PARAMS+=("source=${REAL_HOST_PATH}")
      else
        NEW_PARAMS+=("$param")
      fi
    done
    ADJUSTED_ARGS+=("--mount=$(IFS=,; echo "${NEW_PARAMS[*]}")")
  elif [[ "$arg" == --mount ]]; then
    # Handle space-separated `--mount value`
    PREV_ARG="--mount"
    SKIP_NEXT_ARG=true
  else
    # Pass other arguments as-is
    ADJUSTED_ARGS+=("$arg")
  fi
done

# Inject --cgroup-parent after the subcommand so Docker places new containers
# inside the dedicated Docker cgroup instead of dockerd's own cgroup.
if [[ -n "$INJECT_CGROUP_PARENT" ]]; then
    NEW_ADJUSTED_ARGS=()
    INJECTED=false
    for arg in "${ADJUSTED_ARGS[@]}"; do
        NEW_ADJUSTED_ARGS+=("$arg")
        if [[ "$INJECTED" == "false" ]] && [[ "$arg" == "$DOCKER_SUBCOMMAND" ]]; then
            NEW_ADJUSTED_ARGS+=("--cgroup-parent=${INJECT_CGROUP_PARENT}")
            INJECTED=true
        fi
    done
    ADJUSTED_ARGS=("${NEW_ADJUSTED_ARGS[@]}")
fi

# Debug: Print adjusted arguments
# echo "Real args:" "$(printf "'%s' " "${ADJUSTED_ARGS[@]}")"

# Call the real docker binary with adjusted arguments
exec /usr/bin/docker.real "${ADJUSTED_ARGS[@]}"
