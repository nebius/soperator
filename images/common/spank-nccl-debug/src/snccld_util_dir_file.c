#include "snccld_util_dir_file.h"
#include "snccld_log.h"

#include <errno.h>
#include <fcntl.h>
#include <libgen.h>
#include <limits.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include <sys/stat.h>
#include <sys/types.h>

#include <slurm/spank.h>

spank_err_t snccld_mkdir_p(const char *path, mode_t mode) {
    char  tmp[PATH_MAX + 1] = "";
    char *p                 = NULL;

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

            const mode_t old_mask = umask(0);
            if (mkdir(tmp, mode) != 0 && errno != EEXIST) {
                umask(old_mask);
                return ESPANK_ERROR;
            }
            umask(old_mask);

            *p = '/';
        }
    }

    const mode_t old_mask = umask(0);
    if (mkdir(tmp, mode) != 0 && errno != EEXIST) {
        umask(old_mask);
        return ESPANK_ERROR;
    }
    umask(old_mask);

    return ESPANK_SUCCESS;
}

bool snccld_dir_exists(const char *path) {
    struct stat info;

    if (stat(path, &info) != 0) {
        return false;
    }

    if (S_ISDIR(info.st_mode)) {
        return true;
    }

    return false;
}

void snccld_split_file_path(const char *path, char **dir_out, char **file_out) {
    const char *sep = strrchr(path, '/');
    if (sep) {
        size_t dir_len = sep - path;
        *dir_out       = malloc(dir_len + 1);
        memcpy(*dir_out, path, dir_len);
        (*dir_out)[dir_len] = '\0';

        *file_out = strdup(sep + 1);
    } else {
        *dir_out  = strdup(".");
        *file_out = strdup(path);
    }
}

void snccld_ensure_file_exists(const char *path) {
    char *dir = NULL, *file = NULL;
    snccld_split_file_path(path, &dir, &file);

    snccld_mkdir_p(dir, SNCCLD_DEFAULT_MODE);

    char user_debug_file_absolute[PATH_MAX + 1] = "";
    snprintf(
        user_debug_file_absolute,
        sizeof(user_debug_file_absolute),
        "%s/%s",
        dir,
        file
    );

    const mode_t old_mask           = umask(0);
    const int    user_debug_file_fd = open(
        user_debug_file_absolute,
        O_CREAT | O_WRONLY | O_TRUNC,
        SNCCLD_DEFAULT_MODE
    );
    if (user_debug_file_fd < 0) {
        snccld_log_error("Cannot create file: '%s'", user_debug_file_absolute);
    } else {
        snccld_log_debug("File created: '%s'", user_debug_file_absolute);
        close(user_debug_file_fd);
    }
    umask(old_mask);
}

inline void snccld_ensure_dir_exists(const char *path) {
    snccld_mkdir_p(path, SNCCLD_DEFAULT_MODE);
}
