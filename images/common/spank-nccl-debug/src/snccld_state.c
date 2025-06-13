#include "snccld_state.h"
#include "snccld.h"

#include <fcntl.h>
#include <limits.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include <sys/file.h>

#include <slurm/spank.h>

inline snccld_state_key_t *snccld_key_new() {
    return malloc(sizeof(snccld_state_key_t));
}

spank_err_t snccld_key_get_from(spank_t spank, snccld_state_key_t *key) {
    if (spank_get_item(spank, S_JOB_ID, &key->job_id) != ESPANK_SUCCESS) {
        slurm_error("%s: Failed to get Job ID", SNCCLD_LOG_PREFIX);
        return ESPANK_ERROR;
    }

    if (spank_get_item(spank, S_JOB_STEPID, &key->step_id) != ESPANK_SUCCESS) {
        slurm_error("%s: Failed to get Step ID", SNCCLD_LOG_PREFIX);
        return ESPANK_ERROR;
    }

    return ESPANK_SUCCESS;
}

/**
 * Render file path for the state file.
 *
 * @param key State key.
 * @param hostname Host name.
 *
 * @return Rendered file path.
 */
static char *_snccld_key_to_state_file_path(
    const snccld_state_key_t *key, const char *hostname
) {
    const size_t buf_size = PATH_MAX * sizeof(char);
    char        *res      = malloc(buf_size);

    snprintf(
        res,
        buf_size,
        SNCCLD_TEMPLATE_FILE_NAME,
        SNCCLD_SYSTEM_DIR,
        key->job_id,
        key->step_id,
        hostname,
        "state"
    );

    return res;
}

inline snccld_state_t *snccld_state_new() {
    snccld_state_t *res = malloc(sizeof(snccld_state_t));
    res->fifo_path[0]   = '\0';
    res->log_path[0]    = '\0';
    res->mounts_path[0] = '\0';
    res->tee_pid        = -1;
    return res;
}

/**
 * Calculate a size of the state file in bytes.
 *
 * @return State file size in bytes.
 */
static inline size_t _snccld_state_file_size(void) {
    const size_t max_len_pid_t = 10;
    return (PATH_MAX * 4 + max_len_pid_t + 5) * sizeof(char);
}

char *snccld_state_to_string(const snccld_state_t *state) {
    const size_t buf_size = _snccld_state_file_size();
    char        *res      = malloc(buf_size);

    snprintf(
        res,
        buf_size,
        "%s\n%s\n%s\n%s\n%d\n",
        state->fifo_path,
        state->log_path,
        state->mounts_path,
        state->user_log_path,
        state->tee_pid
    );

    return res;
}

snccld_state_t *snccld_state_from_string(const char *str) {
    if (!str) {
        return NULL;
    }

    snccld_state_t *res = snccld_state_new();

    char *copy    = strdup(str);
    char *saveptr = NULL;
    char *line1   = strtok_r(copy, "\n", &saveptr);
    char *line2   = strtok_r(NULL, "\n", &saveptr);
    char *line3   = strtok_r(NULL, "\n", &saveptr);
    char *line4   = strtok_r(NULL, "\n", &saveptr);
    char *line5   = strtok_r(NULL, "\n", &saveptr);

    if (!line1 || !line2 || !line3 || !line4 || !line5) {
        free(copy);
        free(res);
        return NULL;
    }

    snprintf(res->fifo_path, PATH_MAX, "%s", line1);
    snprintf(res->log_path, PATH_MAX, "%s", line2);
    snprintf(res->mounts_path, PATH_MAX, "%s", line3);
    snprintf(res->user_log_path, PATH_MAX, "%s", line4);
    res->tee_pid = (pid_t)atoi(line5);

    free(copy);

    return res;
}

spank_err_t snccld_state_write(
    const snccld_state_key_t *key, const snccld_state_t *state,
    const char *hostname
) {
    char *path = _snccld_key_to_state_file_path(key, hostname);

    const int fd = open(path, O_CREAT | O_WRONLY, SNCCLD_DEFAULT_MODE);
    if (fd < 0) {
        slurm_error("%s: Cannot open %s: %m", SNCCLD_LOG_PREFIX, path);
        free(path);
        return ESPANK_ERROR;
    }

    if (flock(fd, LOCK_EX | LOCK_NB) != 0) {
        slurm_error("%s: Cannot flock %s: %m", SNCCLD_LOG_PREFIX, path);
        close(fd);
        free(path);
        return ESPANK_ERROR;
    }

    char *state_string = snccld_state_to_string(state);
    ftruncate(fd, 0);
    write(fd, state_string, strlen(state_string));
    free(state_string);

    if (flock(fd, LOCK_UN) != 0) {
        slurm_error("%s: Cannot unflock %s: %m", SNCCLD_LOG_PREFIX, path);
        close(fd);
        free(path);
        return ESPANK_ERROR;
    }

    close(fd);
    free(path);

    return ESPANK_SUCCESS;
}

snccld_state_t *
snccld_state_read(const snccld_state_key_t *key, const char *hostname) {
    snccld_state_t *res  = NULL;
    char           *path = _snccld_key_to_state_file_path(key, hostname);

    const int fd = open(path, O_RDONLY);
    if (fd < 0) {
        slurm_error("%s: Cannot open %s: %m", SNCCLD_LOG_PREFIX, path);
        free(path);
        return NULL;
    }

    if (flock(fd, LOCK_SH) != 0) {
        slurm_error("%s: Cannot flock %s: %m", SNCCLD_LOG_PREFIX, path);
        close(fd);
        free(path);
        return NULL;
    }

    char        *state_string = malloc(_snccld_state_file_size());
    const size_t n = read(fd, state_string, _snccld_state_file_size() - 1);
    state_string[(n > 0) ? n : 0] = '\0';

    res = snccld_state_from_string(state_string);
    free(state_string);
    if (res == NULL) {
        slurm_error(
            "%s: Cannot read state from %s: %m", SNCCLD_LOG_PREFIX, path
        );
    }

    if (flock(fd, LOCK_UN) != 0) {
        slurm_error("%s: Cannot unflock %s: %m", SNCCLD_LOG_PREFIX, path);
        close(fd);
        free(path);
        return res;
    }

    close(fd);
    free(path);

    return res;
}

spank_err_t
snccld_state_cleanup(const snccld_state_key_t *key, const char *hostname) {
    char *path = _snccld_key_to_state_file_path(key, hostname);

    const int res = unlink(path);
    if (res == 0) {
        goto state_cleanup_exit;
    }
    if (errno == ENOENT) {
        // File is already deleted.
        goto state_cleanup_exit;
    }

    slurm_error("%s: Cannot remove '%s': %m", SNCCLD_LOG_PREFIX, path);

    free(path);
    return ESPANK_ERROR;

state_cleanup_exit:
    free(path);
    return ESPANK_SUCCESS;
}
