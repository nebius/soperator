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
    char *res = malloc(PATH_MAX + 1);
    snprintf(
        res,
        PATH_MAX + 1,
        SNCCLD_TEMPLATE_FILE_NAME ".lock",
        SNCCLD_SYSTEM_DIR,
        hostname,
        job_id,
        step_id,
        op
    );
    return res;
}

bool snccld_acquire_lock(
    const uint32_t job_id, const uint32_t step_id, const char *op,
    const char *hostname
) {
    char *path = _snccld_render_lock_file_path(job_id, step_id, op, hostname);
    snccld_log_debug("Acquiring lock: '%s'", path);

    const int fd = open(path, O_CREAT | O_EXCL | O_RDWR, SNCCLD_DEFAULT_MODE);
    if (fd < 0) {
        if (errno == EEXIST) {
            snccld_log_debug("Lock busy/existing: '%s'", path);
        } else {
            snccld_log_error("Cannot create lock '%s': %m", path);
        }
        goto acquire_lock_fail;
    }
    close(fd);

    snccld_log_debug("Lock acquired: '%s'", path);
    free(path);
    return true;

acquire_lock_fail:
    snccld_log_debug("Lock not acquired: '%s'", path);
    free(path);
    return false;
}

void snccld_release_lock(
    const uint32_t job_id, const uint32_t step_id, const char *op,
    const char *hostname
) {
    char *path = _snccld_render_lock_file_path(job_id, step_id, op, hostname);
    snccld_log_debug("Releasing lock: '%s'", path);

    if (unlink(path) != 0 && errno != ENOENT) {
        snccld_log_error("Cannot remove lock '%s': %m", path);
    } else {
        snccld_log_debug("Lock released: '%s'", path);
    }
    free(path);
}
