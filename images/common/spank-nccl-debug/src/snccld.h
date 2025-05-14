#ifndef SNCCLD_H
#define SNCCLD_H

#include <stdint.h>
#include <stdlib.h>
#include <unistd.h>

#include <slurm/spank.h>

SPANK_PLUGIN(nccl_debug, 1);

#define SNCCLDEBUG_ENV_NCCL_DEBUG      "NCCL_DEBUG"
#define SNCCLDEBUG_ENV_NCCL_DEBUG_FILE "NCCL_DEBUG_FILE"

#define SNCCLDEBUG_NCCL_LOG_LEVEL_VERSION "VERSION"
#define SNCCLDEBUG_NCCL_LOG_LEVEL_WARN    "WARN"
#define SNCCLDEBUG_NCCL_LOG_LEVEL_INFO    "INFO"
#define SNCCLDEBUG_NCCL_LOG_LEVEL_TRACE   "TRACE"

#define SNCCLDEBUG_DEFAULT_DIR "/tmp/nccl_debug"

#define SNCCLDEBUG_DEFAULT_FIFO_DIR  SNCCLDEBUG_DEFAULT_DIR
#define SNCCLDEBUG_DEFAULT_FIFO_MODE 0666
#define SNCCLDEBUG_DEFAULT_LOG_DIR   SNCCLDEBUG_DEFAULT_DIR

#define SNCCLDEBUG_LOG_PREFIX "SPANK | NCCL DEBUG: "

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

#endif // SNCCLD_H
