#ifndef SNCCLD_H
#define SNCCLD_H

#include <stdint.h>
#include <stdlib.h>
#include <unistd.h>

#include <slurm/spank.h>

#define SNCCLD_PLUGIN_NAME nccl_debug

#define XSPANK_PLUGIN(__name, __ver) SPANK_PLUGIN(__name, __ver)

#define SNCCLD_NCCL_ENV_DEBUG         "NCCL_DEBUG"
#define SNCCLD_NCCL_ENV_DEBUG_FILE    "NCCL_DEBUG_FILE"
#define SNCCLD_NCCL_LOG_LEVEL_VERSION "VERSION"
#define SNCCLD_NCCL_LOG_LEVEL_WARN    "WARN"
#define SNCCLD_NCCL_LOG_LEVEL_INFO    "INFO"
#define SNCCLD_NCCL_LOG_LEVEL_TRACE   "TRACE"

#define SNCCLD_DEFAULT_DIR "/tmp/nccl_debug"

#define SNCCLD_DEFAULT_FIFO_DIR  SNCCLD_DEFAULT_DIR
#define SNCCLD_DEFAULT_FIFO_MODE 0666

#define SNCCLD_LOG_PREFIX "SPANK | NCCL DEBUG: "

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
