/**
 * Utility functions for directory and file handling.
 */

#ifndef SNCCLD_UTIL_DIR_FILE_H
#define SNCCLD_UTIL_DIR_FILE_H

#include <stdbool.h>

#include <sys/stat.h>

#include <slurm/spank.h>

#define SNCCLD_SYSTEM_DIR         "/tmp/nccl_debug"
#define SNCCLD_DEFAULT_MODE       0777
#define SNCCLD_TEMPLATE_FILE_NAME "%s/%s.%u.%u.%s"

/**
 * Make directory with all its parent directories.
 * The effect is the same as calling `mkdir -p`.
 *
 * @param path Absolute path of the directory to make on.
 *             Trailing slash is handled.
 * @param as_user Whether to create directories as user (w/o umask 0).
 * @param user_gid User GID to set for created directories in case of as_user.
 * @param user_uid User UID to set for created directories in case of as_user.
 *
 * @retval ESPANK_SUCCESS Successfully created the directory.
 * @retval ESPANK_ERROR Something went wrong.
 */
spank_err_t
snccld_mkdir_p(const char *path, bool as_user, gid_t user_gid, uid_t user_uid);

/**
 * Check if the directory exists.
 *
 * @param path Path to the directory to check.
 *
 * @retval true Directory exists.
 * @retval false Directory doesn't exist.
 */
bool snccld_dir_exists(const char *path);

/**
 * Split file path to the directory path and a filename.
 *
 * @param[in] path Path to the file.
 *                 Either absolute or relative.
 * @param[out] dir_out Address of the string where directory path will be
 * stored.
 * @param[out] file_out Address of the string where filename will be stored.
 */
void snccld_split_file_path(const char *path, char **dir_out, char **file_out);

/**
 * Ensure the file exists.
 * If it doesn't exist, it will be created.
 *
 * @param path Path to the file to ensure its existence.
 * @param as_user Whether to create directory and file as user (w/o umask 0).
 * @param user_gid User GID to set for created file in case of as_user.
 * @param user_uid User UID to set for created file in case of as_user.
 */
void snccld_ensure_file_exists(
    const char *path, bool as_user, gid_t user_gid, uid_t user_uid
);

/**
 * Ensure the directory exists.
 * If it doesn't exist, it will be created.
 *
 * @param path Path to the directory to ensure its existence.
 * @param as_user Whether to create directory as user (w/o umask 0).
 * @param user_gid User GID to set for created directories in case of as_user.
 * @param user_uid User UID to set for created directories in case of as_user.
 */
void snccld_ensure_dir_exists(
    const char *path, bool as_user, gid_t user_gid, uid_t user_uid
);

/**
 * Ensure the directory/file has desired mode.
 *
 * @param path Path to the directory/file to ensure its mode.
 * @param mode Directory/file mode.
 *
 * @return ESPANK_SUCCESS, or ESPANK_ERROR.
 */
int snccld_ensure_mode(const char *path, mode_t mode);

#endif // SNCCLD_UTIL_DIR_FILE_H
