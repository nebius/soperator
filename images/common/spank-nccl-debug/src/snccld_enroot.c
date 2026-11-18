#include "snccld_enroot.h"

#include <string.h>

spank_err_t snccld_enroot_clear_mounts(spank_t spank) {
    const spank_err_t state_result =
        spank_unsetenv(spank, SNCCLD_ENROOT_STATE_DIR_ENV);
    const spank_err_t log_result =
        spank_unsetenv(spank, SNCCLD_ENROOT_LOG_DIR_ENV);

    if (state_result != ESPANK_SUCCESS && state_result != ESPANK_ENV_NOEXIST) {
        return state_result;
    }
    if (log_result != ESPANK_SUCCESS && log_result != ESPANK_ENV_NOEXIST) {
        return log_result;
    }
    return ESPANK_SUCCESS;
}

spank_err_t snccld_enroot_set_mounts(
    spank_t spank, const char *state_dir, const char *log_dir
) {
    spank_err_t result;
    if (log_dir[0] != '\0' && strcmp(state_dir, log_dir) != 0) {
        result = spank_setenv(spank, SNCCLD_ENROOT_LOG_DIR_ENV, log_dir, 1);
        if (result != ESPANK_SUCCESS) {
            goto set_mounts_fail;
        }
    }

    // Publish the activation marker only after the optional directory is set.
    result = spank_setenv(spank, SNCCLD_ENROOT_STATE_DIR_ENV, state_dir, 1);
    if (result == ESPANK_SUCCESS) {
        return result;
    }

set_mounts_fail:
    snccld_enroot_clear_mounts(spank);
    return result;
}
