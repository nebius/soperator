#ifndef SNCCLD_UTIL_OPLOCK_H
#define SNCCLD_UTIL_OPLOCK_H

#include <stdbool.h>
#include <stdint.h>

#define SNCCLD_OPLOCK_OP_USER_INIT "user-init"
#define SNCCLD_OPLOCK_OP_TASK_INIT "task-init"
#define SNCCLD_OPLOCK_OP_TASK_EXIT "task-exit"

/**
 * Try to acquire a per‐job/step lock for the given operation on a host.
 *
 * @param job_id: Slurm job ID.
 * @param step_id: Slurm step ID.
 * @param op: Short string identifying the action for the lock.
 * @param hostname: Name of the host where lock is being acquired.
 *
 * @return true if the lock has been acquired, o/w false.
 */
bool snccld_acquire_lock(
    uint32_t job_id, uint32_t step_id, const char *op, const char *hostname
);

/**
 * Release the lock previously acquired for the given operation.
 *
 * @param job_id: Slurm job ID.
 * @param step_id: Slurm step ID.
 * @param op: Short string identifying the action of the lock.
 * @param hostname: Name of the host where lock is being released.
 */
void snccld_release_lock(
    uint32_t job_id, uint32_t step_id, const char *op, const char *hostname
);

#endif // SNCCLD_UTIL_OPLOCK_H
