/*
 * Header-only implementation of missing recursive `mkdir`.
 */

#ifndef SNCCLD_MKDIR_H
#define SNCCLD_MKDIR_H

#include <errno.h>
#include <limits.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>

#include <slurm/spank.h>

/**
 * Recursive `mkdir` call.
 * @param path Path to create a directory on. Trailing slash is handled.
 * @param mode Directory mode.
 * @return ESPANK_SUCCESS if directory was created. Otherwise, ESPANK_ERROR.
 */
static spank_err_t snccld_mkdir_p(const char *path, mode_t mode) {
    char  tmp[PATH_MAX];
    char *p = NULL;

    if (!path || *path == '\0') {
        return ESPANK_ERROR;
    }

    snprintf(tmp, sizeof(tmp), "%s", path);
    const size_t len = strlen(tmp);
    if (tmp[len - 1] == '/') {
        tmp[len - 1] = '\0';
    }

    for (p = tmp + 1; *p; p++) {
        if (*p == '/') {
            *p = '\0';
            if (mkdir(tmp, mode) != 0 && errno != EEXIST) {
                return ESPANK_ERROR;
            }
            *p = '/';
        }
    }

    if (mkdir(tmp, mode) != 0 && errno != EEXIST) {
        return ESPANK_ERROR;
    }

    return ESPANK_SUCCESS;
}

#endif // SNCCLD_MKDIR_H
