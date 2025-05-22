/*
 * State handling.
 */

#ifndef SNCCLD_STATE_H
#define SNCCLD_STATE_H

#include "snccld.h"

#include <fcntl.h>
#include <limits.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include <unistd.h>

#include <sys/file.h>

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

static char *snccld_key_to_state_file_path(const snccld_state_key_t *key) {
    const size_t buf_size = PATH_MAX * sizeof(char);
    char        *res      = malloc(buf_size);

    snprintf(
        res,
        buf_size,
        "%s/%u-%u.%s",
        SNCCLD_DEFAULT_DIR,
        key->job_id,
        key->step_id,
        "state"
    );

    return res;
}

typedef struct {
    char  fifo_path[PATH_MAX];
    char  log_path[PATH_MAX];
    pid_t tee_pid;
} snccld_state_t;

static inline snccld_state_t *snccld_state_new(void) {
    return malloc(sizeof(snccld_state_t));
}

static inline size_t snccld_state_file_size(void) {
    const size_t max_len_pid_t = 10;
    return (PATH_MAX * 2 + max_len_pid_t + 3) * sizeof(char);
}

static char *snccld_state_to_string(const snccld_state_t *state) {
    const size_t buf_size = snccld_state_file_size();
    char        *res      = malloc(buf_size);

    snprintf(
        res,
        buf_size,
        "%s\n%s\n%d\n",
        state->fifo_path,
        state->log_path,
        state->tee_pid
    );

    return res;
}

static snccld_state_t *snccld_state_from_string(const char *str) {
    if (!str) {
        return NULL;
    }

    snccld_state_t *res = snccld_state_new();

    char *copy    = strdup(str);
    char *saveptr = NULL;
    char *line1   = strtok_r(copy, "\n", &saveptr);
    char *line2   = strtok_r(NULL, "\n", &saveptr);
    char *line3   = strtok_r(NULL, "\n", &saveptr);

    if (!line1 || !line2 || !line3) {
        free(copy);
        free(res);
        return NULL;
    }

    snprintf(res->fifo_path, PATH_MAX, "%s", line1);
    snprintf(res->log_path, PATH_MAX, "%s", line2);
    res->tee_pid = (pid_t)atoi(line3);

    free(copy);

    return res;
}

static spank_err_t
snccld_state_write(const snccld_state_key_t *key, const snccld_state_t *state) {
    char *path = snccld_key_to_state_file_path(key);

    const int fd =
        open(path, O_CREAT | O_EXCL | O_WRONLY, SNCCLD_DEFAULT_FIFO_MODE);
    if (fd < 0) {
        if (errno == EEXIST) {
            // Another process already wrote the state - do nothing.
            return ESPANK_SUCCESS;
        }

        slurm_error(SNCCLD_LOG_PREFIX "Cannot open %s: %m", path);
        free(path);
        return ESPANK_ERROR;
    }

    if (flock(fd, LOCK_EX) != 0) {
        slurm_error(SNCCLD_LOG_PREFIX "Cannot flock %s: %m", path);
        close(fd);
        free(path);
        return ESPANK_ERROR;
    }

    char *state_string = snccld_state_to_string(state);
    ftruncate(fd, 0);
    write(fd, state_string, strlen(state_string));
    free(state_string);

    if (flock(fd, LOCK_UN) != 0) {
        slurm_error(SNCCLD_LOG_PREFIX "Cannot unflock %s: %m", path);
        close(fd);
        free(path);
        return ESPANK_ERROR;
    }

    close(fd);
    free(path);

    return ESPANK_SUCCESS;
}

static snccld_state_t *snccld_state_read(const snccld_state_key_t *key) {
    snccld_state_t *res  = NULL;
    char           *path = snccld_key_to_state_file_path(key);

    const int fd = open(path, O_RDONLY);
    if (fd < 0) {
        slurm_error(SNCCLD_LOG_PREFIX "Cannot open %s: %m", path);
        free(path);
        return NULL;
    }

    if (flock(fd, LOCK_SH) != 0) {
        slurm_error(SNCCLD_LOG_PREFIX "Cannot flock %s: %m", path);
        close(fd);
        free(path);
        return NULL;
    }

    char        *state_string = malloc(snccld_state_file_size());
    const size_t n = read(fd, state_string, snccld_state_file_size() - 1);
    state_string[(n > 0) ? n : 0] = '\0';

    res = snccld_state_from_string(state_string);
    free(state_string);
    if (res == NULL) {
        slurm_error(SNCCLD_LOG_PREFIX "Cannot read state from %s: %m", path);
    }

    if (flock(fd, LOCK_UN) != 0) {
        slurm_error(SNCCLD_LOG_PREFIX "Cannot unflock %s: %m", path);
        close(fd);
        free(path);
        return res;
    }

    close(fd);
    free(path);

    return res;
}

static spank_err_t snccld_state_cleanup(snccld_state_key_t *key) {
    char *path = snccld_key_to_state_file_path(key);

    int res = unlink(path);
    if (res == 0) {
        free(path);
        return ESPANK_SUCCESS;
    }

    if (errno == ENOENT) {
        // File is already deleted.
        return ESPANK_SUCCESS;
    }

    slurm_error(SNCCLD_LOG_PREFIX "Cannot remove %s: %m", path);
    free(path);

    return ESPANK_ERROR;
}

#endif // SNCCLD_STATE_H
