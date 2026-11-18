#include "snccld_enroot.h"

#include <assert.h>
#include <stdio.h>
#include <string.h>

static char state_dir[4096];
static char log_dir[4096];
static const char *fail_set;
static const char *fail_unset;

static char *slot(const char *name) {
    if (strcmp(name, SNCCLD_ENROOT_STATE_DIR_ENV) == 0) {
        return state_dir;
    }
    assert(strcmp(name, SNCCLD_ENROOT_LOG_DIR_ENV) == 0);
    return log_dir;
}

spank_err_t spank_setenv(spank_t spank, const char *name, const char *value, int overwrite) {
    (void)spank;
    assert(overwrite == 1);
    if (fail_set && strcmp(name, fail_set) == 0) {
        return ESPANK_ERROR;
    }
    snprintf(slot(name), 4096, "%s", value);
    return ESPANK_SUCCESS;
}

spank_err_t spank_unsetenv(spank_t spank, const char *name) {
    (void)spank;
    if (fail_unset && strcmp(name, fail_unset) == 0) {
        return ESPANK_ERROR;
    }
    char *value = slot(name);
    if (value[0] == '\0') {
        return ESPANK_ENV_NOEXIST;
    }
    value[0] = '\0';
    return ESPANK_SUCCESS;
}

int main(void) {
    assert(snccld_enroot_clear_mounts(NULL) == ESPANK_SUCCESS);
    assert(snccld_enroot_set_mounts(NULL, "/tmp/state", "/logs/worker-0") == ESPANK_SUCCESS);
    assert(strcmp(state_dir, "/tmp/state") == 0);
    assert(strcmp(log_dir, "/logs/worker-0") == 0);
    assert(snccld_enroot_clear_mounts(NULL) == ESPANK_SUCCESS);
    assert(state_dir[0] == '\0' && log_dir[0] == '\0');

    assert(snccld_enroot_set_mounts(NULL, "/tmp/state", "") == ESPANK_SUCCESS);
    assert(log_dir[0] == '\0');
    assert(snccld_enroot_clear_mounts(NULL) == ESPANK_SUCCESS);
    assert(snccld_enroot_set_mounts(NULL, "/tmp/state", "/tmp/state") == ESPANK_SUCCESS);
    assert(log_dir[0] == '\0');
    assert(snccld_enroot_clear_mounts(NULL) == ESPANK_SUCCESS);

    for (int i = 0; i < 2; ++i) {
        fail_set = i == 0 ? SNCCLD_ENROOT_LOG_DIR_ENV : SNCCLD_ENROOT_STATE_DIR_ENV;
        assert(snccld_enroot_set_mounts(NULL, "/tmp/state", "/logs") == ESPANK_ERROR);
        assert(state_dir[0] == '\0' && log_dir[0] == '\0');
    }
    fail_set = NULL;

    for (int i = 0; i < 2; ++i) {
        assert(snccld_enroot_set_mounts(NULL, "/tmp/state", "/logs") == ESPANK_SUCCESS);
        fail_unset = i == 0 ? SNCCLD_ENROOT_STATE_DIR_ENV : SNCCLD_ENROOT_LOG_DIR_ENV;
        assert(snccld_enroot_clear_mounts(NULL) == ESPANK_ERROR);
        assert(i == 0 ? log_dir[0] == '\0' : state_dir[0] == '\0');
        fail_unset = NULL;
        assert(snccld_enroot_clear_mounts(NULL) == ESPANK_SUCCESS);
    }
    puts("SPANK mount environment: publication, deduplication, reset and error rollback passed");
    return 0;
}
