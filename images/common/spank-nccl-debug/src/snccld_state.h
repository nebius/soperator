/**
 * @brief State management.
 *
 * This module defines the types and functions for creating, serializing,
 * reading, and cleaning up per-job/step state used by the plugin.
 */

#ifndef SNCCLD_STATE_H
#define SNCCLD_STATE_H

#include <limits.h>
#include <stdint.h>
#include <unistd.h>

#include <sys/file.h>

#include <slurm/spank.h>

/// Key of the state presented by the Slurm job and step IDs.
typedef struct {
    /// Slurm job ID.
    uint32_t job_id;

    /// Slurm job step ID.
    uint32_t step_id;
} snccld_state_key_t;

/**
 * Create new state key.
 *
 * @return Initialized state key.
 */
snccld_state_key_t *snccld_key_new(void);

/**
 * Get state key from the SPANK context.
 *
 * @param[in]  spank  SPANK context.
 * @param[out] key    State key to store gathered info.
 *
 * @retval ESPANK_SUCCESS Successfully retrieved the key.
 * @retval ESPANK_ERROR Something went wrong.
 */
spank_err_t snccld_key_get_from(spank_t spank, snccld_state_key_t *key);

/// State of the plugin for the particular job.
typedef struct {
    /// Job submitter GID.
    gid_t user_gid;

    /// Job submitter UID.
    uid_t user_uid;

    /// Absolute path of the named pipe (FIFO).
    char fifo_path[PATH_MAX + 1];

    /// Absolute path of the file to store the NCCL debug output.
    char log_path[PATH_MAX + 1];

    /// Absolute path of the Enroot mount configuration.
    char mounts_path[PATH_MAX + 1];

    /**
     * Absolute path of the file to store the NCCL debug output set by user
     * via NCCL_DEBUG_FILE.
     */
    char user_log_path[PATH_MAX + 1];

    /// PID of the FIFO reading process.
    pid_t tee_pid;
} snccld_state_t;

/**
 * Create new state.
 *
 * @return Initialized state.
 */
snccld_state_t *snccld_state_new(void);

/**
 * Represent state as a string.
 *
 * @param state State to be represented.
 *
 * @return String representation of the state.
 */
char *snccld_state_to_string(const snccld_state_t *state);

/**
 * Retrieve state from its string representation.
 *
 * @param str String representation of the state.
 *
 * @return State retrieved from its string representation.
 */
snccld_state_t *snccld_state_from_string(const char *str);

/**
 * Write state to the file.
 *
 * @param key State key.
 * @param state State to write.
 * @param hostname Host name.
 *
 * @retval ESPANK_SUCCESS Successfully wrote the state.
 * @retval ESPANK_ERROR Something went wrong.
 */
spank_err_t snccld_state_write(
    const snccld_state_key_t *key, const snccld_state_t *state,
    const char *hostname
);

/**
 * Read state from the file.
 *
 * @param key State key.
 * @param hostname Host name.
 * @return
 */
snccld_state_t *
snccld_state_read(const snccld_state_key_t *key, const char *hostname);

/**
 * Clean up the state file.
 *
 * @param key State key.
 * @param hostname Host name.
 *
 * @retval ESPANK_SUCCESS Successfully cleaned up the state file.
 * @retval ESPANK_ERROR Something went wrong.
 */
spank_err_t
snccld_state_cleanup(const snccld_state_key_t *key, const char *hostname);

#endif // SNCCLD_STATE_H
