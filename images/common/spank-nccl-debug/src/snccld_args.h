/// Plugin argument and option handling.

#ifndef SNCCLD_ARGS_H
#define SNCCLD_ARGS_H

#include "snccld_log.h"
#include "snccld_nccl.h"
#include "snccld_util_dir_file.h"
#include "snccld_util_string.h"

#include <ctype.h>
#include <limits.h>
#include <stdbool.h>
#include <string.h>
#include <strings.h>

#include <slurm/spank.h>

/// Error message for invalid argument value.
#define SNCCLD_LOG_TEMPLATE_INVALID_ARG                                        \
    "Invalid value for argument '%s': '%s', using default '%s'"

/// Argument prefix.
#define SNCCLD_ARG_PREFIX "nccld"

/// Possible values for boolean argument shown in its info.
#define SNCCLD_ARG_BOOLEAN_STRING_ARGINFO "(1 | True) | (0 | False)"

#pragma region Argument enabled
/// `enabled` argument name.
#define SNCCLD_ARG_ENABLED "enabled"
/// `enabled` argument env var.
#define SNCCLD_ARG_ENABLED_ENV "SNCCLD_ENABLED"
/// `enabled` argument info.
#define SNCCLD_ARG_ENABLED_ARGINFO SNCCLD_ARG_BOOLEAN_STRING_ARGINFO
/// `enabled` argument default value.
#define SNCCLD_ARG_ENABLED_DEFAULT false
// clang-format off
/// `enabled` argument usage description.
#define SNCCLD_ARG_ENABLED_USAGE                                               \
    "whether to enable " XSTR(SNCCLD_PLUGIN_NAME) " plugin. "                  \
    "Possible values are case-insensitive. "                                   \
    SNCCLD_ARG_ENABLED_ENV " env var is also supported."
// clang-format on
#pragma endregion

#pragma region Argument log level
/// `log-level` argument name.
#define SNCCLD_ARG_LOG_LEVEL "log-level"
/// `log-level` argument env var.
#define SNCCLD_ARG_LOG_LEVEL_ENV "SNCCLD_LOG_LEVEL"
/// `log-level` argument info.
#define SNCCLD_ARG_LOG_LEVEL_ARGINFO "LOG_LEVEL"
/// `log-level` argument default value.
#define SNCCLD_ARG_LOG_LEVEL_DEFAULT SNCCLD_NCCL_LOG_LEVEL_INFO
// clang-format off
/// `log-level` argument usage description.
#define SNCCLD_ARG_LOG_LEVEL_USAGE                                             \
    "log level to be forced. "                                                 \
    "Possible values are: "                                                    \
    SNCCLD_NCCL_LOG_LEVEL_VERSION ", "                                         \
    SNCCLD_NCCL_LOG_LEVEL_WARN ", "                                            \
    SNCCLD_NCCL_LOG_LEVEL_INFO ", "                                            \
    SNCCLD_NCCL_LOG_LEVEL_TRACE ". "                                           \
    "Possible values are case-insensitive. "                                   \
    SNCCLD_ARG_LOG_LEVEL_ENV " env var is also supported."
// clang-format on
#pragma endregion

#pragma region Argument out dir
/// `out-dir` argument name.
#define SNCCLD_ARG_OUT_DIR "out-dir"
/// `out-dir` argument env var.
#define SNCCLD_ARG_OUT_DIR_ENV "SNCCLD_OUT_DIR"
/// `out-dir` argument info.
#define SNCCLD_ARG_OUT_DIR_ARGINFO "PATH"
/// `out-dir` argument default value.
#define SNCCLD_ARG_OUT_DIR_DEFAULT SNCCLD_SYSTEM_DIR
// clang-format off
/// `out-dir` argument usage description.
#define SNCCLD_ARG_OUT_DIR_USAGE                                               \
    "path to the directory to store `" SNCCLD_NCCL_ENV_DEBUG "` outputs. "        \
    SNCCLD_ARG_OUT_DIR_ENV " env var is also supported."
// clang-format on
#pragma endregion

#pragma region Argument out file
/// `out-file` argument name.
#define SNCCLD_ARG_OUT_FILE "out-file"
/// `out-file` argument env var.
#define SNCCLD_ARG_OUT_FILE_ENV "SNCCLD_OUT_FILE"
/// `out-file` argument info.
#define SNCCLD_ARG_OUT_FILE_ARGINFO SNCCLD_ARG_BOOLEAN_STRING_ARGINFO
/// `out-file` argument default value.
#define SNCCLD_ARG_OUT_FILE_DEFAULT true
// clang-format off
/// `out-file` argument usage description.
#define SNCCLD_ARG_OUT_FILE_USAGE                                                  \
"whether to additionally redirect `" SNCCLD_NCCL_ENV_DEBUG "` outputs to the file. " \
"Possible values are case-insensitive. "                                         \
SNCCLD_ARG_OUT_FILE_ENV " env var is also supported."
// clang-format on
#pragma endregion

#pragma region Argument out stdout
/// `out-stdout` argument name.
#define SNCCLD_ARG_OUT_STDOUT "out-stdout"
/// `out-stdout` argument env var.
#define SNCCLD_ARG_OUT_STDOUT_ENV "SNCCLD_OUT_STDOUT"
/// `out-stdout` argument info.
#define SNCCLD_ARG_OUT_STDOUT_ARGINFO SNCCLD_ARG_BOOLEAN_STRING_ARGINFO
/// `out-stdout` argument default value.
#define SNCCLD_ARG_OUT_STDOUT_DEFAULT true
// clang-format off
/// `out-stdout` argument usage description.
#define SNCCLD_ARG_OUT_STDOUT_USAGE                                                  \
    "whether to additionally redirect `" SNCCLD_NCCL_ENV_DEBUG "` outputs to stdout. " \
    "Possible values are case-insensitive. "                                         \
    SNCCLD_ARG_OUT_STDOUT_ENV " env var is also supported."
// clang-format on
#pragma endregion

/// Parse argument `__arg` with `__parse_fn` if it starts with `__arg_name`.
#define SNCCLD_PARSE_ARG(__arg, __arg_name, __parse_fn)                        \
    if (strncmp(__arg, __arg_name, strlen(__arg_name)) == 0) {                 \
        __parse_fn(__arg);                                                     \
        continue;                                                              \
    }

/// Parse argument with `__parse_fn` if `__env_key` is defined.
#define SNCCLD_PARSE_ENV_ARG(__env_key, __parse_fn)                            \
    do {                                                                       \
        char val[PATH_MAX + 1];                                                \
        if (spank_getenv(spank, __env_key, val, sizeof(val)) ==                \
            ESPANK_SUCCESS) {                                                  \
            __parse_fn(val);                                                   \
        }                                                                      \
    } while (false);

/// Validate `__optarg` with name `__arg` and handle its value with `__handler`.
#define SNCCLD_ARG_OPTION(__arg, __handler, __optarg)                          \
    if (__optarg == NULL || *__optarg == '\0') {                               \
        snccld_log_error(                                                      \
            "--" SNCCLD_ARG_PREFIX "-" __arg ": argument required"             \
        );                                                                     \
        return ESPANK_BAD_ARG;                                                 \
    }                                                                          \
                                                                               \
    __handler(__optarg);                                                       \
                                                                               \
    return ESPANK_SUCCESS;

/// Per-job plugin config.
typedef struct {
    bool enabled;
    char log_level[8];
    char out_dir[PATH_MAX + 1];
    bool out_file;
    bool out_stdout;
} snccld_config_t;

/// Per-job plugin config initialized with default values.
static snccld_config_t snccld_config = {
    .enabled    = SNCCLD_ARG_ENABLED_DEFAULT,
    .log_level  = SNCCLD_ARG_LOG_LEVEL_DEFAULT,
    .out_dir    = SNCCLD_ARG_OUT_DIR_DEFAULT,
    .out_file   = SNCCLD_ARG_OUT_FILE_DEFAULT,
    .out_stdout = SNCCLD_ARG_OUT_STDOUT_DEFAULT,
};

/**
 * Parse and validate value of the `enabled` argument.
 *
 * @param val `enabled` argument value.
 */
static void snccld_parse_arg_enabled_value(const char *val) {
    if (strcasecmp(val, "1") == 0 || strcasecmp(val, "true") == 0) {
        snccld_config.enabled = true;
        return;
    }

    if (strcasecmp(val, "0") == 0 || strcasecmp(val, "false") == 0) {
        snccld_config.enabled = false;
        return;
    }

    snccld_log_error(
        SNCCLD_LOG_TEMPLATE_INVALID_ARG,
        SNCCLD_ARG_ENABLED,
        val,
        snccld_config.enabled ? "true" : "false"
    );
}

/**
 * Extract and parse value of the `enabled` argument.
 *
 * @param arg full `enabled` argument specification.
 */
static inline void snccld_parse_arg_enabled(const char *arg) {
    const char *val = arg + strlen(SNCCLD_ARG_ENABLED "=");
    snccld_parse_arg_enabled_value(val);
}

/**
 * Implementation of `enabled` option registration callback.
 *
 * @related spank_opt_cb_f
 */
static int spank_option_enabled(int val, const char *optarg, int remote) {
    SNCCLD_ARG_OPTION(
        SNCCLD_ARG_ENABLED, snccld_parse_arg_enabled_value, optarg
    )
}

/**
 * Parse and validate value of the `log-level` argument.
 *
 * @param val `log-level` argument value.
 */
static void snccld_parse_arg_log_level_value(const char *val) {
    if (strcasecmp(val, SNCCLD_NCCL_LOG_LEVEL_VERSION) == 0 ||
        strcasecmp(val, SNCCLD_NCCL_LOG_LEVEL_WARN) == 0 ||
        strcasecmp(val, SNCCLD_NCCL_LOG_LEVEL_INFO) == 0 ||
        strcasecmp(val, SNCCLD_NCCL_LOG_LEVEL_TRACE) == 0) {
        strncpy(
            snccld_config.log_level, val, sizeof(snccld_config.log_level) - 1
        );
        for (size_t i = 0; i < sizeof(val) - 1; i++) {
            snccld_config.log_level[i] = toupper(snccld_config.log_level[i]);
        }
        snccld_config.log_level[sizeof(snccld_config.log_level) - 1] = '\0';
        return;
    }

    snccld_log_error(
        SNCCLD_LOG_TEMPLATE_INVALID_ARG,
        SNCCLD_ARG_LOG_LEVEL,
        val,
        snccld_config.log_level
    );
}

/**
 * Extract and parse value of the `log-level` argument.
 *
 * @param arg full `log-level` argument specification.
 */
static inline void snccld_parse_arg_log_level(const char *arg) {
    const char *val = arg + strlen(SNCCLD_ARG_LOG_LEVEL "=");
    snccld_parse_arg_log_level_value(val);
}

/**
 * Implementation of `log-level` option registration callback.
 *
 * @related spank_opt_cb_f
 */
static int spank_option_log_level(int val, const char *optarg, int remote) {
    SNCCLD_ARG_OPTION(
        SNCCLD_ARG_LOG_LEVEL, snccld_parse_arg_log_level_value, optarg
    )
}

/**
 * Parse and validate value of the `out-dir` argument.
 *
 * @param val `out-dir` argument value.
 */
static void snccld_parse_arg_out_dir_value(const char *val) {
    strncpy(snccld_config.out_dir, val, sizeof(snccld_config.out_dir) - 1);
    snccld_config.out_dir[sizeof(snccld_config.out_dir) - 1] = '\0';
}

/**
 * Extract and parse value of the `out-dir` argument.
 *
 * @param arg full `out-dir` argument specification.
 */
static inline void snccld_parse_arg_out_dir(const char *arg) {
    const char *val = arg + strlen(SNCCLD_ARG_OUT_DIR "=");
    snccld_parse_arg_out_dir_value(val);
}

/**
 * Implementation of `out-dir` option registration callback.
 *
 * @related spank_opt_cb_f
 */
static int spank_option_out_dir(int val, const char *optarg, int remote) {
    SNCCLD_ARG_OPTION(
        SNCCLD_ARG_OUT_DIR, snccld_parse_arg_out_dir_value, optarg
    )
}

/**
 * Parse and validate value of the `out-file` argument.
 *
 * @param val `out-file` argument value.
 */
static void snccld_parse_arg_out_file_value(const char *val) {
    if (strcasecmp(val, "1") == 0 || strcasecmp(val, "true") == 0) {
        snccld_config.out_file = true;
        return;
    }

    if (strcasecmp(val, "0") == 0 || strcasecmp(val, "false") == 0) {
        snccld_config.out_file = false;
        return;
    }

    snccld_log_error(
        SNCCLD_LOG_TEMPLATE_INVALID_ARG,
        SNCCLD_ARG_OUT_FILE,
        val,
        snccld_config.out_file ? "true" : "false"
    );
}

/**
 * Extract and parse value of the `out-file` argument.
 *
 * @param arg full `out-file` argument specification.
 */
static inline void snccld_parse_arg_out_file(const char *arg) {
    const char *val = arg + strlen(SNCCLD_ARG_OUT_FILE "=");
    snccld_parse_arg_out_file_value(val);
}

/**
 * Implementation of `out-file` option registration callback.
 *
 * @related spank_opt_cb_f
 */
static int spank_option_out_file(int val, const char *optarg, int remote) {
    SNCCLD_ARG_OPTION(
        SNCCLD_ARG_OUT_FILE, snccld_parse_arg_out_file_value, optarg
    )
}

/**
 * Parse and validate value of the `out-stdout` argument.
 *
 * @param val `out-stdout` argument value.
 */
static void snccld_parse_arg_out_stdout_value(const char *val) {
    if (strcasecmp(val, "1") == 0 || strcasecmp(val, "true") == 0) {
        snccld_config.out_stdout = true;
        return;
    }

    if (strcasecmp(val, "0") == 0 || strcasecmp(val, "false") == 0) {
        snccld_config.out_stdout = false;
        return;
    }

    snccld_log_error(
        SNCCLD_LOG_TEMPLATE_INVALID_ARG,
        SNCCLD_ARG_OUT_STDOUT,
        val,
        snccld_config.out_stdout ? "true" : "false"
    );
}

/**
 * Extract and parse value of the `out-stdout` argument.
 *
 * @param arg full `out-stdout` argument specification.
 */
static inline void snccld_parse_arg_out_stdout(const char *arg) {
    const char *val = arg + strlen(SNCCLD_ARG_OUT_STDOUT "=");
    snccld_parse_arg_out_stdout_value(val);
}

/**
 * Implementation of `out-stdout` option registration callback.
 *
 * @related spank_opt_cb_f
 */
static int spank_option_out_stdout(int val, const char *optarg, int remote) {
    SNCCLD_ARG_OPTION(
        SNCCLD_ARG_OUT_STDOUT, snccld_parse_arg_out_stdout_value, optarg
    )
}

/**
 * Parse arguments from env.
 *
 * @param spank SPANK context.
 */
static void snccld_parse_env_vars(spank_t spank) {
    SNCCLD_PARSE_ENV_ARG(
        SNCCLD_ARG_ENABLED_ENV, snccld_parse_arg_enabled_value
    );
    SNCCLD_PARSE_ENV_ARG(
        SNCCLD_ARG_LOG_LEVEL_ENV, snccld_parse_arg_log_level_value
    );
    SNCCLD_PARSE_ENV_ARG(
        SNCCLD_ARG_OUT_DIR_ENV, snccld_parse_arg_out_dir_value
    );
    SNCCLD_PARSE_ENV_ARG(
        SNCCLD_ARG_OUT_FILE_ENV, snccld_parse_arg_out_file_value
    );
    SNCCLD_PARSE_ENV_ARG(
        SNCCLD_ARG_OUT_STDOUT_ENV, snccld_parse_arg_out_stdout_value
    );
}

/**
 * Parse plugin arguments from `argv`, then from env.
 *
 * @param spank SPANK context.
 * @param argc Argument count.
 * @param argv Argument values.
 */
static void snccld_parse_plugin_args(spank_t spank, int argc, char **argv) {
    for (int i = 0; i < argc; ++i) {
        const char *arg = argv[i];

        // clang-format off
        SNCCLD_PARSE_ARG(arg, SNCCLD_ARG_ENABLED, snccld_parse_arg_enabled);
        SNCCLD_PARSE_ARG(arg, SNCCLD_ARG_LOG_LEVEL, snccld_parse_arg_log_level);
        SNCCLD_PARSE_ARG(arg, SNCCLD_ARG_OUT_DIR, snccld_parse_arg_out_dir);
        SNCCLD_PARSE_ARG(arg, SNCCLD_ARG_OUT_FILE, snccld_parse_arg_out_file);
        SNCCLD_PARSE_ARG(arg, SNCCLD_ARG_OUT_STDOUT, snccld_parse_arg_out_stdout);
        // clang-format on

        snccld_log_error("Unknown plugin arg: %s", arg);
    }

    snccld_parse_env_vars(spank);
}

/// SPANK plugin option table.
struct spank_option spank_opts[] = {
    {
        .name    = SNCCLD_ARG_PREFIX "-" SNCCLD_ARG_ENABLED,
        .arginfo = SNCCLD_ARG_ENABLED_ARGINFO,
        .usage   = "[" XSTR(SNCCLD_PLUGIN_NAME) "] " SNCCLD_ARG_ENABLED_USAGE,
        .has_arg = true,
        .val     = 0,
        .cb      = spank_option_enabled,
    },
    {
        .name    = SNCCLD_ARG_PREFIX "-" SNCCLD_ARG_LOG_LEVEL,
        .arginfo = SNCCLD_ARG_LOG_LEVEL_ARGINFO,
        .usage   = "[" XSTR(SNCCLD_PLUGIN_NAME) "] " SNCCLD_ARG_LOG_LEVEL_USAGE,
        .has_arg = true,
        .val     = 0,
        .cb      = spank_option_log_level,
    },
    {
        .name    = SNCCLD_ARG_PREFIX "-" SNCCLD_ARG_OUT_DIR,
        .arginfo = SNCCLD_ARG_OUT_DIR_ARGINFO,
        .usage   = "[" XSTR(SNCCLD_PLUGIN_NAME) "] " SNCCLD_ARG_OUT_DIR_USAGE,
        .has_arg = true,
        .val     = 0,
        .cb      = spank_option_out_dir,
    },
    {
        .name    = SNCCLD_ARG_PREFIX "-" SNCCLD_ARG_OUT_FILE,
        .arginfo = SNCCLD_ARG_OUT_FILE_ARGINFO,
        .usage   = "[" XSTR(SNCCLD_PLUGIN_NAME) "] " SNCCLD_ARG_OUT_FILE_USAGE,
        .has_arg = true,
        .val     = 0,
        .cb      = spank_option_out_file,
    },
    {
        .name    = SNCCLD_ARG_PREFIX "-" SNCCLD_ARG_OUT_STDOUT,
        .arginfo = SNCCLD_ARG_OUT_STDOUT_ARGINFO,
        .usage = "[" XSTR(SNCCLD_PLUGIN_NAME) "] " SNCCLD_ARG_OUT_STDOUT_USAGE,
        .has_arg = true,
        .val     = 0,
        .cb      = spank_option_out_stdout,
    },
    SPANK_OPTIONS_TABLE_END
};

/**
 * Register plugin arguments as SPANK options.
 *
 * @param spank SPANK context.
 *
 * @retval ESPANK_SUCCESS Successfully registered options.
 * @retval ESPANK_ERROR Something went wrong.
 */
static spank_err_t snccld_args_register(spank_t spank) {
    spank_err_t res = ESPANK_SUCCESS;

    for (int i = 0; spank_opts[i].name != NULL; ++i) {
        res = spank_option_register(spank, &spank_opts[i]);
        if (res != ESPANK_SUCCESS) {
            snccld_log_error(
                "Cannot register option %s: %s",
                spank_opts[i].name,
                spank_strerror(res)
            );
            return ESPANK_ERROR;
        }
    }

    return res;
}

#endif // SNCCLD_ARGS_H
