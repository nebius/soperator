#include "snccld_util_dir_file.h"

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
    char *path_copy_dir = strdup(path);
    char *real_dir_copy = realpath(dirname(path_copy_dir), NULL);
    *dir_out            = strdup(real_dir_copy);
    free(real_dir_copy);
    free(path_copy_dir);

    char *path_copy_file = strdup(path);
    *file_out            = strdup(basename(path_copy_file));
    free(path_copy_file);
}

void snccld_ensure_file_exists(const char *path) {
    char *dir, *file;
    snccld_split_file_path(path, &dir, &file);

    snccld_mkdir_p(dir, SNCCLD_DEFAULT_MODE);

    char user_debug_file_absolute[PATH_MAX] = "";
    snprintf(
        user_debug_file_absolute,
        sizeof(user_debug_file_absolute),
        "%s/%s",
        dir,
        file
    );

    const int user_debug_file_fd =
        open(user_debug_file_absolute, O_CREAT | O_WRONLY, SNCCLD_DEFAULT_MODE);
    if (user_debug_file_fd > 0) {
        close(user_debug_file_fd);
    }
}

inline void snccld_ensure_dir_exists(const char *path) {
    snccld_mkdir_p(path, SNCCLD_DEFAULT_MODE);
}
