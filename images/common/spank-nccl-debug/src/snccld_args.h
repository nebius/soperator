/*
 * Plugin arguments and flags handling.
 */

#ifndef SNCCLD_ARGS_H
#define SNCCLD_ARGS_H

#include "snccld_log.h"
#include "snccld_nccl.h"
#include "snccld_util_dir_file.h"

#include <ctype.h>
#include <limits.h>
#include <stdbool.h>
#include <string.h>
#include <strings.h>

#include <slurm/spank.h>

#define STR(__x)  #__x
#define XSTR(__x) STR(__x)

#define SNCCLD_LOG_TEMPLATE_INVALID_ARG                                        \
    "Invalid value for argument '%s': '%s', using default '%s'"

#define SNCCLD_ARG_PREFIX "nccld"

#define SNCCLD_ARG_BOOLEAN_STRING_ARGINFO "(1 | True) | (0 | False)"

#define SNCCLD_ARG_ENABLED         "enabled"
#define SNCCLD_ARG_ENABLED_ENV     "SNCCLD_ENABLED"
#define SNCCLD_ARG_ENABLED_ARGINFO SNCCLD_ARG_BOOLEAN_STRING_ARGINFO
#define SNCCLD_ARG_ENABLED_DEFAULT false
// clang-format off

#define SNCCLD_ARG_ENABLED_USAGE                                               \
    "whether to enable " XSTR(SNCCLD_PLUGIN_NAME) " plugin. "                  \
    "Possible values are case-insensitive. "                                   \
    SNCCLD_ARG_ENABLED_ENV " env var is also supported."
// clang-format on

#define SNCCLD_ARG_LOG_LEVEL         "log-level"
#define SNCCLD_ARG_LOG_LEVEL_ENV     "SNCCLD_LOG_LEVEL"
#define SNCCLD_ARG_LOG_LEVEL_ARGINFO "LOG_LEVEL"
#define SNCCLD_ARG_LOG_LEVEL_DEFAULT SNCCLD_NCCL_LOG_LEVEL_INFO
// clang-format off

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

#define SNCCLD_ARG_OUT_DIR         "out-dir"
#define SNCCLD_ARG_OUT_DIR_ENV     "SNCCLD_OUT_DIR"
#define SNCCLD_ARG_OUT_DIR_ARGINFO "PATH"
#define SNCCLD_ARG_OUT_DIR_DEFAULT SNCCLD_SYSTEM_DIR
// clang-format off

#define SNCCLD_ARG_OUT_DIR_USAGE                                               \
    "path to the directory to store `" SNCCLD_NCCL_ENV_DEBUG "` outputs. "        \
    SNCCLD_ARG_OUT_DIR_ENV " env var is also supported."
// clang-format on

#define SNCCLD_ARG_OUT_FILE         "out-file"
#define SNCCLD_ARG_OUT_FILE_ENV     "SNCCLD_OUT_FILE"
#define SNCCLD_ARG_OUT_FILE_ARGINFO SNCCLD_ARG_BOOLEAN_STRING_ARGINFO
#define SNCCLD_ARG_OUT_FILE_DEFAULT true
// clang-format off

#define SNCCLD_ARG_OUT_FILE_USAGE                                                  \
"whether to additionally redirect `" SNCCLD_NCCL_ENV_DEBUG "` outputs to the file. " \
"Possible values are case-insensitive. "                                         \
SNCCLD_ARG_OUT_FILE_ENV " env var is also supported."
// clang-format on

#define SNCCLD_ARG_OUT_STDOUT         "out-stdout"
#define SNCCLD_ARG_OUT_STDOUT_ENV     "SNCCLD_OUT_STDOUT"
#define SNCCLD_ARG_OUT_STDOUT_ARGINFO SNCCLD_ARG_BOOLEAN_STRING_ARGINFO
#define SNCCLD_ARG_OUT_STDOUT_DEFAULT true
// clang-format off

#define SNCCLD_ARG_OUT_STDOUT_USAGE                                                  \
    "whether to additionally redirect `" SNCCLD_NCCL_ENV_DEBUG "` outputs to stdout. " \
    "Possible values are case-insensitive. "                                         \
    SNCCLD_ARG_OUT_STDOUT_ENV " env var is also supported."
// clang-format on

#define SNCCLD_PARSE_ARG(arg, arg_name, parse_fn)                              \
    if (strncmp(arg, arg_name, strlen(arg_name)) == 0) {                       \
        parse_fn(arg);                                                         \
        continue;                                                              \
    }

#define SNCCLD_PARSE_ENV_ARG(env_key, parse_fn)                                \
    do {                                                                       \
        char val[PATH_MAX];                                                    \
        if (spank_getenv(spank, env_key, val, sizeof(val)) ==                  \
            ESPANK_SUCCESS) {                                                  \
            parse_fn(val);                                                     \
        }                                                                      \
    } while (false);

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

typedef struct {
    bool enabled;
    char log_level[8];
    char out_dir[PATH_MAX];
    bool out_file;
    bool out_stdout;
} snccld_config_t;

static snccld_config_t snccld_config = {
    .enabled    = SNCCLD_ARG_ENABLED_DEFAULT,
    .log_level  = SNCCLD_ARG_LOG_LEVEL_DEFAULT,
    .out_dir    = SNCCLD_ARG_OUT_DIR_DEFAULT,
    .out_file   = SNCCLD_ARG_OUT_FILE_DEFAULT,
    .out_stdout = SNCCLD_ARG_OUT_STDOUT_DEFAULT,
};

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

static inline void snccld_parse_arg_enabled(const char *arg) {
    const char *val = arg + strlen(SNCCLD_ARG_ENABLED "=");
    snccld_parse_arg_enabled_value(val);
}

/**
 * Implementation of @spank_opt_cb_f callback for `enabled` arg.
 */
static int spank_option_enabled(int val, const char *optarg, int remote) {
    SNCCLD_ARG_OPTION(
        SNCCLD_ARG_ENABLED, snccld_parse_arg_enabled_value, optarg
    )
}

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

static inline void snccld_parse_arg_log_level(const char *arg) {
    const char *val = arg + strlen(SNCCLD_ARG_LOG_LEVEL "=");
    snccld_parse_arg_log_level_value(val);
}

/**
 * Implementation of @spank_opt_cb_f callback for `log-level` arg.
 */
static int spank_option_log_level(int val, const char *optarg, int remote) {
    SNCCLD_ARG_OPTION(
        SNCCLD_ARG_LOG_LEVEL, snccld_parse_arg_log_level_value, optarg
    )
}

static void snccld_parse_arg_out_dir_value(const char *val) {
    strncpy(snccld_config.out_dir, val, sizeof(snccld_config.out_dir) - 1);
    snccld_config.out_dir[sizeof(snccld_config.out_dir) - 1] = '\0';
}

static inline void snccld_parse_arg_out_dir(const char *arg) {
    const char *val = arg + strlen(SNCCLD_ARG_OUT_DIR "=");
    snccld_parse_arg_out_dir_value(val);
}

/**
 * Implementation of @spank_opt_cb_f callback for `out-dir` arg.
 */
static int spank_option_out_dir(int val, const char *optarg, int remote) {
    SNCCLD_ARG_OPTION(
        SNCCLD_ARG_OUT_DIR, snccld_parse_arg_out_dir_value, optarg
    )
}

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

static inline void snccld_parse_arg_out_file(const char *arg) {
    const char *val = arg + strlen(SNCCLD_ARG_OUT_FILE "=");
    snccld_parse_arg_out_file_value(val);
}

/**
 * Implementation of @spank_opt_cb_f callback for `out-file` arg.
 */
static int spank_option_out_file(int val, const char *optarg, int remote) {
    SNCCLD_ARG_OPTION(
        SNCCLD_ARG_OUT_FILE, snccld_parse_arg_out_file_value, optarg
    )
}

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

static inline void snccld_parse_arg_out_stdout(const char *arg) {
    const char *val = arg + strlen(SNCCLD_ARG_OUT_STDOUT "=");
    snccld_parse_arg_out_stdout_value(val);
}

/**
 * Implementation of @spank_opt_cb_f callback for `out-stdout` arg.
 */
static int spank_option_out_stdout(int val, const char *optarg, int remote) {
    SNCCLD_ARG_OPTION(
        SNCCLD_ARG_OUT_STDOUT, snccld_parse_arg_out_stdout_value, optarg
    )
}

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
