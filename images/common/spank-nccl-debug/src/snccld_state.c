#include "snccld_state.h"

#include "snccld_log.h"
#include "snccld_util_dir_file.h"

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
        snccld_log_error("Failed to get Job ID");
        return ESPANK_ERROR;
    }

    if (spank_get_item(spank, S_JOB_STEPID, &key->step_id) != ESPANK_SUCCESS) {
        snccld_log_error("Failed to get Step ID");
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
    const size_t buf_size = PATH_MAX + 1;
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
    snccld_state_t *res   = malloc(sizeof(snccld_state_t));
    res->fifo_path[0]     = '\0';
    res->log_path[0]      = '\0';
    res->mounts_path[0]   = '\0';
    res->user_log_path[0] = '\0';
    res->tee_pid          = -1;

    return res;
}

/**
 * Calculate a size of the state file in bytes.
 *
 * @return State file size in bytes.
 */
static inline size_t _snccld_state_file_size(void) {
    const size_t max_len_pid_t = 10;
    return PATH_MAX * 4 + max_len_pid_t + 5 + 1;
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
    snccld_state_t *res = snccld_state_new();

    char *copy = strdup(str);
    char *p    = copy;
    for (int line = 0; line < 5; ++line) {
        char *field = strsep(&p, "\n");
        if (!field) {
            continue;
        }
        switch (line) {
            case 0:
                snprintf(res->fifo_path, PATH_MAX + 1, "%s", field);
                break;
            case 1:
                snprintf(res->log_path, PATH_MAX + 1, "%s", field);
                break;
            case 2:
                snprintf(res->mounts_path, PATH_MAX + 1, "%s", field);
                break;
            case 3:
                snprintf(res->user_log_path, PATH_MAX + 1, "%s", field);
                break;
            case 4:
                res->tee_pid = (pid_t)atoi(field);
                break;
        }
    }
    free(copy);

    return res;
}

spank_err_t snccld_state_write(
    const snccld_state_key_t *key, const snccld_state_t *state,
    const char *hostname
) {
    char *path = _snccld_key_to_state_file_path(key, hostname);
    snccld_log_debug("Writing state file: '%s'", path);

    const int fd = open(path, O_CREAT | O_TRUNC | O_RDWR, SNCCLD_DEFAULT_MODE);
    if (fd < 0) {
        snccld_log_error("Cannot open or truncate state file '%s': %m", path);
        goto state_write_fail;
    }

    char *state_string = snccld_state_to_string(state);
    ftruncate(fd, 0);
    write(fd, state_string, strlen(state_string));
    free(state_string);

    snccld_log_debug("State file written: '%s'", path);
    close(fd);
    free(path);
    return ESPANK_SUCCESS;

state_write_fail:
    snccld_log_debug("State file not written: '%s'", path);
    free(path);
    return ESPANK_ERROR;
}

snccld_state_t *
snccld_state_read(const snccld_state_key_t *key, const char *hostname) {
    snccld_state_t *res  = NULL;
    char           *path = _snccld_key_to_state_file_path(key, hostname);
    snccld_log_debug("Reading state file: '%s'", path);

    const int fd = open(path, O_RDONLY);
    if (fd < 0) {
        if (errno == ENOENT) {
            // This could be an error,
            // but we don't want excess logs because of races.
            snccld_log_debug("State file does not exist: '%s'", path);
        } else {
            snccld_log_error("Cannot read state file '%s': %m", path);
        }
        goto state_read_exit;
    }

    char        *state_string = malloc(_snccld_state_file_size());
    const size_t n = read(fd, state_string, _snccld_state_file_size() - 1);
    state_string[(n > 0) ? n : 0] = '\0';
    close(fd);

    res = snccld_state_from_string(state_string);
    free(state_string);

    snccld_log_debug("State file read: '%s'", path);

state_read_exit:
    free(path);
    return res;
}

spank_err_t
snccld_state_cleanup(const snccld_state_key_t *key, const char *hostname) {
    char *path = _snccld_key_to_state_file_path(key, hostname);
    snccld_log_debug("Cleaning up state file '%s': %m", path);

    const int res = unlink(path);
    if (res != 0 && errno != ENOENT) {
        snccld_log_error("Cannot clean up state file '%s': %m", path);
        goto state_cleanup_fail;
    }

    snccld_log_debug("State file cleaned up: '%s'", path);
    free(path);
    return ESPANK_SUCCESS;

state_cleanup_fail:
    free(path);
    return ESPANK_ERROR;
}
