/// NCCL definitions.

#ifndef SNCCLD_NCCL_H
#define SNCCLD_NCCL_H

/**
 * NCCL's env var for output log file.
 *
 * @see
 * https://docs.nvidia.com/deeplearning/nccl/user-guide/docs/env.html#nccl-debug-file
 */
#define SNCCLD_NCCL_ENV_DEBUG_FILE "NCCL_DEBUG_FILE"

/**
 * NCCL's env var for output log level.
 *
 * @see
 * https://docs.nvidia.com/deeplearning/nccl/user-guide/docs/env.html#nccl-debug
 */
#define SNCCLD_NCCL_ENV_DEBUG "NCCL_DEBUG"

/**
 * NCCL's `version` log level.
 *
 * @related SNCCLD_NCCL_ENV_DEBUG
 */
#define SNCCLD_NCCL_LOG_LEVEL_VERSION "VERSION"

/**
 * NCCL's `warning` log level.
 *
 * @related SNCCLD_NCCL_ENV_DEBUG
 */
#define SNCCLD_NCCL_LOG_LEVEL_WARN "WARN"

/**
 * NCCL's `info` log level.
 *
 * @related SNCCLD_NCCL_ENV_DEBUG
 */
#define SNCCLD_NCCL_LOG_LEVEL_INFO "INFO"

/**
 * NCCL's `trace` log level.
 *
 * @related SNCCLD_NCCL_ENV_DEBUG
 */
#define SNCCLD_NCCL_LOG_LEVEL_TRACE "TRACE"

#endif // SNCCLD_NCCL_H
