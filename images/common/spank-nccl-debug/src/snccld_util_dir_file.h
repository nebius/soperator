/**
 * Utility functions for directory and file handling.
 */

#ifndef SNCCLD_UTIL_DIR_FILE_H
#define SNCCLD_UTIL_DIR_FILE_H

#include <stdbool.h>

#include <sys/stat.h>

#include <slurm/spank.h>

#define SNCCLD_SYSTEM_DIR         "/tmp/nccl_debug"
#define SNCCLD_DEFAULT_MODE       0666
#define SNCCLD_TEMPLATE_FILE_NAME "%s/%u-%u.%s.%s"

/**
 * Make directory with all its parent directories.
 * The effect is the same as calling `mkdir -p`.
 *
 * @param path Absolute path of the directory to make on.
 *             Trailing slash is handled.
 * @param mode Directory mode.
 *
 * @retval ESPANK_SUCCESS Successfully created the directory.
 * @retval ESPANK_ERROR Something went wrong.
 */
spank_err_t snccld_mkdir_p(const char *path, mode_t mode);

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
 */
void snccld_ensure_file_exists(const char *path);

/**
 * Ensure the directory exists.
 * If it doesn't exist, it will be created.
 *
 * @param path Path to the directory to ensure its existence.
 */
void snccld_ensure_dir_exists(const char *path);

#endif // SNCCLD_UTIL_DIR_FILE_H
