/*
 * Header-only implementation of missing recursive `mkdir`.
 */

#ifndef SNCCLD_MKDIR_H
#define SNCCLD_MKDIR_H

#include <errno.h>
#include <limits.h>
#include <stdbool.h>
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

/**
 * Check if the directory exists.
 * @param path Path to the directory to check.
 * @return true if directory exists. Otherwise, false.
 */
static bool snccld_dir_exists(const char *path) {
    struct stat info;

    if (stat(path, &info) != 0) {
        return false;
    }

    if (S_ISDIR(info.st_mode)) {
        return true;
    }

    return false;
}

#define SNCCLD_CURRENT_DIR "."

/**
 * Split file path to the directory path and a filename.
 * @param path Path to the file.
 * @param dir_out Address of the string where directory path will be stored.
 * @param file_out Address of the string where filename will be stored.
 */
static void
snccld_split_file_path(const char *path, char **dir_out, char **file_out) {
    const char *sep = strrchr(path, '/');
    if (sep) {
        size_t dir_len = sep - path;

        *dir_out = malloc(dir_len + 1);
        memcpy(*dir_out, path, dir_len);
        (*dir_out)[dir_len] = '\0';

        *file_out = strdup(sep + 1);
    } else {
        // No slash: current directory
        *dir_out  = strdup(SNCCLD_CURRENT_DIR);
        *file_out = strdup(path);
    }
}

/**
 * Ensure if file exists. Creates the file if it doesn't exist.
 * @param path Path to the file to ensure its existence.
 */
static void snccld_ensure_file_exists(const char *path) {
    char *dir, *file;

    snccld_split_file_path(path, &dir, &file);

    if (dir != SNCCLD_CURRENT_DIR) {
        snccld_mkdir_p(dir, SNCCLD_DEFAULT_MODE);
    } else {
        getcwd(dir, PATH_MAX);
    }

    char user_debug_file_absolute[PATH_MAX] = "";
    snprintf(
        user_debug_file_absolute,
        sizeof(user_debug_file_absolute),
        "%s/%s",
        dir,
        file
    );
    free(dir);
    free(file);

    const int user_debug_file_fd =
        open(user_debug_file_absolute, O_CREAT | O_WRONLY, SNCCLD_DEFAULT_MODE);
    if (user_debug_file_fd > 0) {
        close(user_debug_file_fd);
    }
}

/**
 * Ensure if directory exists. Creates the directory if it doesn't exist.
 * @param path Path to the directory to ensure its existence.
 */
static inline void snccld_ensure_dir_exists(const char *path) {
    snccld_mkdir_p(path, SNCCLD_DEFAULT_MODE);
}

#endif // SNCCLD_MKDIR_H
