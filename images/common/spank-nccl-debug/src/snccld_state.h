/*
 * State handling.
 */

#ifndef SNCCLD_STATE_H
#define SNCCLD_STATE_H

#include <stdint.h>
#include <stdlib.h>

#include <slurm/spank.h>

typedef struct {
    uint32_t job_id;
    uint32_t step_id;
} snccld_state_key_t;

static inline snccld_state_key_t *snccld_key_new(void) {
    return malloc(sizeof(snccld_state_key_t));
}

static spank_err_t snccld_key_get_from(spank_t spank, snccld_state_key_t *key) {
    if (spank_get_item(spank, S_JOB_ID, &key->job_id) != ESPANK_SUCCESS) {
        slurm_error(SNCCLD_LOG_PREFIX "Failed to get Job ID");
        return ESPANK_ERROR;
    }

    if (spank_get_item(spank, S_JOB_STEPID, &key->step_id) != ESPANK_SUCCESS) {
        slurm_error(SNCCLD_LOG_PREFIX "Failed to get Step ID");
        return ESPANK_ERROR;
    }

    return ESPANK_SUCCESS;
}

typedef struct {
    snccld_state_key_t key;
    char               fifo_path[256];
    char               log_path[256];
    pid_t              tee_pid;
} snccld_state_t;

static inline snccld_state_t *snccld_state_new() {
    return malloc(sizeof(snccld_state_t));
}

#endif // SNCCLD_STATE_H
