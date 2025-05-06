#ifndef SNCCLD_H
#define SNCCLD_H

#include <stdint.h>
#include <stdlib.h>
#include <unistd.h>

#include <slurm/spank.h>

SPANK_PLUGIN(nccl_debug, 1);

#define SNCCLDEBUG_LOG_PREFIX "SPANK | NCCL DEBUG: "

typedef struct {
    uint32_t job_id;
    uint32_t step_id;
    pid_t tee_pid;
    char pipe_name[64];
} snccld_output_info_t;

// ReSharper disable once CppRedundantInlineSpecifier
// ReSharper disable once CppDFAUnreachableFunctionCall
static inline snccld_output_info_t *snccld_output_info_new(const uint64_t job_id, const uint64_t step_id) {
    snccld_output_info_t *info = malloc(sizeof(snccld_output_info_t));
    info->job_id = job_id;
    info->step_id = step_id;
    return info;
}

// ReSharper disable once CppRedundantInlineSpecifier
static inline snccld_output_info_t *snccld_output_info_get_from(const spank_t spank) {
    uint32_t job_id = 0, step_id = 0;
    spank_get_item(spank, S_JOB_ID, &job_id);
    spank_get_item(spank, S_JOB_STEPID, &step_id);

    return snccld_output_info_new(job_id, step_id);
}

#endif //SNCCLD_H
