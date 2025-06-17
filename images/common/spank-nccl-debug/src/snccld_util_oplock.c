#include "snccld_util_oplock.h"

#include "snccld_log.h"
#include "snccld_util_dir_file.h"

#include <fcntl.h>
#include <limits.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

#include <sys/file.h>

/**
 * Renders a file path to the lock file for a per‐job/step lock for the given
 * operation on a host.
 *
 * @param job_id: Slurm job ID.
 * @param step_id: Slurm step ID.
 * @param op: Short string identifying the action for the lock.
 * @param hostname: Name of the host where lock is being acquired.
 *
 * @return Rendered lock file path.
 */
static inline char *_snccld_render_lock_file_path(
    const uint32_t job_id, const uint32_t step_id, const char *op,
    const char *hostname
) {
    char *res = malloc(sizeof(char) * PATH_MAX + 1);
    snprintf(
        res,
        PATH_MAX,
        SNCCLD_TEMPLATE_FILE_NAME ".lock",
        SNCCLD_SYSTEM_DIR,
        job_id,
        step_id,
        hostname,
        op
    );
    return res;
}

bool snccld_acquire_lock(
    const uint32_t job_id, const uint32_t step_id, const char *op,
    const char *hostname
) {
    char *lock_file_path =
        _snccld_render_lock_file_path(job_id, step_id, op, hostname);
    snccld_log_debug("lock_path=%s", lock_file_path);

    const int lock_fd =
        open(lock_file_path, O_CREAT | O_WRONLY, SNCCLD_DEFAULT_MODE);
    if (lock_fd < 0) {
        snccld_log_error("Cannot open '%s': %m", lock_file_path);
        goto acquire_lock_fail;
    }

    if (flock(lock_fd, LOCK_EX | LOCK_NB) == -1) {
        snccld_log_error("Cannot flock '%s': %m", lock_file_path);
        goto acquire_lock_fail;
    }
    close(lock_fd);

    free(lock_file_path);
    return true;

acquire_lock_fail:
    free(lock_file_path);
    return false;
}

void snccld_release_lock(
    const uint32_t job_id, const uint32_t step_id, const char *op,
    const char *hostname
) {
    char *lock_file_path =
        _snccld_render_lock_file_path(job_id, step_id, op, hostname);
    snccld_log_debug("lock_path=%s", lock_file_path);

    const int lock_fd =
        open(lock_file_path, O_CREAT | O_WRONLY, SNCCLD_DEFAULT_MODE);
    if (lock_fd < 0) {
        snccld_log_error("Cannot open '%s': %m", lock_file_path);
        goto release_lock_fail;
    }

    if (flock(lock_fd, LOCK_UN) != 0) {
        snccld_log_error("Cannot unflock '%s': %m", lock_file_path);
        goto release_lock_fail;
    }
    close(lock_fd);

    unlink(lock_file_path);
    free(lock_file_path);

    return;

release_lock_fail:
    free(lock_file_path);
}
