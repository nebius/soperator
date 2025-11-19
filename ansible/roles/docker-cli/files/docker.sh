#!/bin/bash

BASE_DIR="/mnt/jail"  # Base directory for chroot

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

# Debug: Print adjusted arguments
# echo "Real args:" "$(printf "'%s' " "${ADJUSTED_ARGS[@]}")"

# Call the real docker binary with adjusted arguments
exec /usr/bin/docker.real "${ADJUSTED_ARGS[@]}"
