#ifndef SNCCLD_H
#define SNCCLD_H

#include <limits.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include <unistd.h>

#include <slurm/spank.h>

#define SNCCLD_ARG_ENABLED        "enabled"
#define SNCCLD_ARG_ENABLED_ENV    "SNCCLD_ENABLED"
#define SNCCLD_ARG_LOG_LEVEL      "log_level"
#define SNCCLD_ARG_LOG_LEVEL_ENV  "SNCCLD_LOG_LEVEL"
#define SNCCLD_ARG_OUT_DIR        "out_dir"
#define SNCCLD_ARG_OUT_DIR_ENV    "SNCCLD_OUT_DIR"
#define SNCCLD_ARG_OUT_STDOUT     "out_stdout"
#define SNCCLD_ARG_OUT_STDOUT_ENV "SNCCLD_OUT_STDOUT"

#define SNCCLD_NCCL_ENV_DEBUG         "NCCL_DEBUG"
#define SNCCLD_NCCL_ENV_DEBUG_FILE    "NCCL_DEBUG_FILE"
#define SNCCLD_NCCL_LOG_LEVEL_VERSION "VERSION"
#define SNCCLD_NCCL_LOG_LEVEL_WARN    "WARN"
#define SNCCLD_NCCL_LOG_LEVEL_INFO    "INFO"
#define SNCCLD_NCCL_LOG_LEVEL_TRACE   "TRACE"

#define SNCCLD_DEFAULT_DIR "/tmp/nccl_debug"

#define SNCCLD_DEFAULT_FIFO_DIR  SNCCLD_DEFAULT_DIR
#define SNCCLD_DEFAULT_FIFO_MODE 0666
#define SNCCLD_DEFAULT_LOG_DIR   SNCCLD_DEFAULT_DIR

#define SNCCLD_LOG_PREFIX "SPANK | NCCL DEBUG: "
#define SNCCLD_LOG_INVALID_ARG                                                 \
    "Invalid value for argument '%s': '%s', using default '%s'"

typedef struct {
    bool enabled;
    char log_level[8];
    char out_dir[PATH_MAX];
    bool out_stdout;
} snccld_config_t;

typedef struct {
    uint32_t job_id;
    uint32_t step_id;
} snccld_output_info_key_t;

snccld_output_info_key_t *snccld_new_key(void);

spank_err_t snccld_get_key_from(spank_t, snccld_output_info_key_t *);

typedef struct {
    snccld_output_info_key_t key;
    char                     fifo_path[256];
    char                     log_path[256];
    pid_t                    tee_pid;
} snccld_output_info_t;

snccld_output_info_t *snccld_new_info(void);

#define SNCCLD_PARSE_ARG(arg, arg_name, parse_fn)                              \
    if (strncmp(arg, arg_name, strlen(arg_name)) == 0) {                       \
        parse_fn(arg);                                                         \
        continue;                                                              \
    }

#define SNCCLD_PARSE_ENV_ARG(env_key, parse_fn)                                \
    do {                                                                       \
        char val[PATH_MAX];                                                    \
        if (spank_getenv(spank, env_key, val, sizeof(val)) ==                  \
            ESPANK_SUCCESS) {                                                  \
            parse_fn(val);                                                     \
        }                                                                      \
    } while (false);

#endif // SNCCLD_H
