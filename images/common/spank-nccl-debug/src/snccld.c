#include "snccld.h"
#include "snccld_mkdir.h"

#include <ctype.h>
#include <fcntl.h>
#include <sched.h>
#include <signal.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include <sys/mount.h>
#include <sys/stat.h>
#include <sys/wait.h>

#include <slurm/slurm.h>
#include <slurm/slurm_errno.h>
#include <slurm/spank.h>

char *get_executable_name(pid_t pid) {
    static char buffer[256];
    snprintf(buffer, sizeof(buffer), "/proc/%d/exe", pid);
    ssize_t len = readlink(buffer, buffer, sizeof(buffer) - 1);
    if (len != -1) {
        buffer[len] = '\0';
        return buffer;
    } else {
        return "unknown";
    }
}

void log_context(const char *func_name, spank_t spank) {
    char            context[16];
    spank_context_t spank_ctx = spank_context();

    switch (spank_ctx) {
        case S_CTX_ERROR:
            strcpy(context, "error");
            break;
        case S_CTX_LOCAL:
            strcpy(context, "local");
            break;
        case S_CTX_REMOTE:
            strcpy(context, "remote");
            break;
        case S_CTX_ALLOCATOR:
            strcpy(context, "allocator");
            break;
        case S_CTX_SLURMD:
            strcpy(context, "slurmd");
            break;
        case S_CTX_JOB_SCRIPT:
            strcpy(context, "job_script");
            break;
        default:
            strcpy(context, "unknown");
    }

    pid_t pid         = getpid();
    pid_t ppid        = getppid();
    char *pname       = get_executable_name(pid);
    char *parent_name = get_executable_name(ppid);

    uint32_t job_id = 0, job_stepid = 0;
    pid_t    task_pid = 0;

    spank_get_item(spank, S_JOB_ID, &job_id);
    spank_get_item(spank, S_JOB_STEPID, &job_stepid);
    spank_get_item(spank, S_TASK_PID, &task_pid);

    slurm_spank_log(
        SNCCLD_LOG_PREFIX "%s\t%s\t%d\t%s\t%d\t%s\t%u\t%u\t%d",
        func_name,
        context,
        pid,
        pname,
        ppid,
        parent_name,
        job_id,
        job_stepid,
        task_pid
    );
}

static snccld_output_info_t *infos[64];
static size_t                infos_count = 0;

char *snccld_format_infos() {
    if (infos_count == 0 || infos[0] == NULL) {
        return strdup("[]");
    }

    size_t buf_size = 256 * infos_count + 3;
    char  *result   = malloc(buf_size);
    if (!result) {
        return NULL;
    }

    size_t offset  = 0;
    offset        += snprintf(result + offset, buf_size - offset, "[");

    for (size_t i = 0; i < infos_count; ++i) {
        offset += snprintf(
            result + offset,
            buf_size - offset,
            "(job=%u, step=%u, pipe=%s, log=%s, tee=%u)%s",
            infos[i]->key.job_id,
            infos[i]->key.step_id,
            infos[i]->fifo_path,
            infos[i]->log_path,
            infos[i]->tee_pid,
            (i < infos_count - 1) ? ", " : ""
        );
    }

    snprintf(result + offset, buf_size - offset, "]");
    return result;
}

XSPANK_PLUGIN(SNCCLD_PLUGIN_NAME, 1);

static snccld_config_t snccld_config = {
    .enabled    = SNCCLD_ARG_ENABLED_DEFAULT,
    .log_level  = SNCCLD_NCCL_LOG_LEVEL_INFO,
    .out_dir    = SNCCLD_ARG_OUT_DIR_DEFAULT,
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

    slurm_error(
        SNCCLD_LOG_PREFIX SNCCLD_LOG_INVALID_ARG,
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
    slurm_error(SNCCLD_LOG_PREFIX "Val: %s", val);
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

    slurm_error(
        SNCCLD_LOG_PREFIX SNCCLD_LOG_INVALID_ARG,
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
 * Implementation of @spank_opt_cb_f callback for `log_level` arg.
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
 * Implementation of @spank_opt_cb_f callback for `out_dir` arg.
 */
static int spank_option_out_dir(int val, const char *optarg, int remote) {
    SNCCLD_ARG_OPTION(
        SNCCLD_ARG_OUT_DIR, snccld_parse_arg_out_dir_value, optarg
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

    slurm_error(
        SNCCLD_LOG_PREFIX SNCCLD_LOG_INVALID_ARG,
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
 * Implementation of @spank_opt_cb_f callback for `out_stdout` arg.
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
        SNCCLD_PARSE_ARG(arg, SNCCLD_ARG_OUT_STDOUT, snccld_parse_arg_out_stdout);
        // clang-format on

        slurm_error(SNCCLD_LOG_PREFIX "Unknown plugin arg: %s", arg);
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
            slurm_error(
                SNCCLD_LOG_PREFIX "Couldn't register option %s: %s",
                spank_opts[i].name,
                spank_strerror(res)
            );
            return ESPANK_ERROR;
        }
    }

    return res;
}

int slurm_spank_init(spank_t spank, int argc, char **argv) {
    log_context("init", spank);

    switch (spank_context()) {
        case S_CTX_LOCAL:
        case S_CTX_REMOTE:
            {
                // To read from plugstack.conf, then from env
                snccld_parse_plugin_args(spank, argc, argv);
                // To read from flags
                return snccld_args_register(spank);
            }
        default:
            return ESPANK_SUCCESS;
    }
}

int slurm_spank_user_init(spank_t spank, int argc, char **argv) {
    log_context("user_init", spank);

    if (spank_context() != S_CTX_REMOTE) {
        return ESPANK_SUCCESS;
    }

    slurm_error(
        SNCCLD_LOG_PREFIX "Config:\n"
                          "\t" SNCCLD_ARG_ENABLED ": %s\n"
                          "\t" SNCCLD_ARG_LOG_LEVEL ": %s\n"
                          "\t" SNCCLD_ARG_OUT_DIR ": %s\n"
                          "\t" SNCCLD_ARG_OUT_STDOUT ": %s",
        snccld_config.enabled ? "true" : "false",
        snccld_config.log_level,
        snccld_config.out_dir,
        snccld_config.out_stdout ? "true" : "false"
    );

    if (!snccld_config.enabled) {
        return ESPANK_SUCCESS;
    }

    snccld_output_info_key_t *key = snccld_new_key();
    if (snccld_get_key_from(spank, key) != ESPANK_SUCCESS ||
        key->step_id == SLURM_BATCH_SCRIPT) {
        free(key);
        return ESPANK_SUCCESS;
    }

    snccld_output_info_t *info = snccld_new_info();
    info->key                  = *key;
    free(key);

    char debug_val[16]  = "";
    int  user_set_debug = 0;
    if (spank_getenv(
            spank, SNCCLD_NCCL_ENV_DEBUG, debug_val, sizeof(debug_val)
        ) == ESPANK_SUCCESS) {
        user_set_debug = 1;
    }

    if (snccld_mkdir_p(SNCCLD_DEFAULT_DIR, SNCCLD_DEFAULT_FIFO_MODE) ==
        ESPANK_ERROR) {
        slurm_error(
            SNCCLD_LOG_PREFIX "Cannot create directory '%s': %m",
            SNCCLD_DEFAULT_DIR
        );
        free(info);
        return ESPANK_ERROR;
    }
    snprintf(
        info->fifo_path,
        sizeof(info->fifo_path),
        "%s/%u_%u.fifo",
        SNCCLD_DEFAULT_DIR,
        info->key.job_id,
        info->key.step_id
    );

    if (snccld_mkdir_p(snccld_config.out_dir, SNCCLD_DEFAULT_FIFO_MODE) ==
        ESPANK_ERROR) {
        slurm_error(
            SNCCLD_LOG_PREFIX "Cannot create directory '%s': %m",
            snccld_config.out_dir
        );
        free(info);
        return ESPANK_ERROR;
    }
    snprintf(
        info->log_path,
        sizeof(info->log_path),
        "%s/%u_%u.out",
        snccld_config.out_dir,
        info->key.job_id,
        info->key.step_id
    );

    if (mkfifo(info->fifo_path, SNCCLD_DEFAULT_FIFO_MODE) != EXIT_SUCCESS) {
        if (errno == EEXIST) {
            unlink(info->fifo_path);
            if (mkfifo(info->fifo_path, SNCCLD_DEFAULT_FIFO_MODE) !=
                EXIT_SUCCESS) {
                slurm_error(
                    SNCCLD_LOG_PREFIX "Cannot create FIFO %s: %m",
                    info->fifo_path
                );
                free(info);
                return ESPANK_SUCCESS;
            }
        } else {
            slurm_error(
                SNCCLD_LOG_PREFIX "Cannot create FIFO %s: %m", info->fifo_path
            );
            free(info);
            return ESPANK_SUCCESS;
        }
    }

    pid_t pid = fork();
    if (pid < 0) {
        slurm_error(SNCCLD_LOG_PREFIX "fork() failed: %m");
        unlink(info->fifo_path);
        return ESPANK_SUCCESS;
    } else if (pid == 0) {
        int fd = open(info->fifo_path, O_RDONLY);
        if (fd < 0) {
            _exit(EXIT_FAILURE);
        }
        dup2(fd, STDIN_FILENO);
        close(fd);

        if (!user_set_debug) {
            int devnull = open("/dev/null", O_WRONLY);
            if (devnull >= 0) {
                dup2(devnull, STDOUT_FILENO);
                close(devnull);
            }
        }

        execlp(
            "stdbuf",
            "stdbuf",
            "-oL",
            "/usr/bin/tee",
            "-a",
            info->log_path,
            (char *)NULL
        );
        _exit(EXIT_FAILURE);
    }

    info->tee_pid        = pid;
    infos[infos_count++] = info;

    char *str = snccld_format_infos();
    slurm_spank_log(SNCCLD_LOG_PREFIX "added new info: %s", str);
    slurm_spank_log(SNCCLD_LOG_PREFIX "info count: %lu", infos_count);
    free(str);

    if (!user_set_debug) {
        slurm_spank_log(
            SNCCLD_LOG_PREFIX "Setting %s=%s",
            SNCCLD_NCCL_ENV_DEBUG,
            snccld_config.log_level
        );
        spank_setenv(spank, SNCCLD_NCCL_ENV_DEBUG, snccld_config.log_level, 1);
    } else {
        slurm_spank_log(SNCCLD_LOG_PREFIX "Skipping env var");
    }

    {
        slurm_spank_log(
            SNCCLD_LOG_PREFIX "Setting %s=%s",
            SNCCLD_NCCL_ENV_DEBUG_FILE,
            info->fifo_path
        );
        spank_setenv(spank, SNCCLD_NCCL_ENV_DEBUG_FILE, info->fifo_path, 1);
    }

    return ESPANK_SUCCESS;
}

int slurm_spank_task_exit(spank_t spank, int argc, char **argv) {
    log_context("task_exit", spank);

    if (spank_context() != S_CTX_REMOTE) {
        return ESPANK_SUCCESS;
    }

    slurm_error(
        SNCCLD_LOG_PREFIX "Config:\n"
                          "\t" SNCCLD_ARG_ENABLED ": %s\n"
                          "\t" SNCCLD_ARG_LOG_LEVEL ": %s\n"
                          "\t" SNCCLD_ARG_OUT_DIR ": %s\n"
                          "\t" SNCCLD_ARG_OUT_STDOUT ": %s",
        snccld_config.enabled ? "true" : "false",
        snccld_config.log_level,
        snccld_config.out_dir,
        snccld_config.out_stdout ? "true" : "false"
    );

    if (!snccld_config.enabled) {
        return ESPANK_SUCCESS;
    }

    snccld_output_info_key_t *key = snccld_new_key();
    if (snccld_get_key_from(spank, key) != ESPANK_SUCCESS ||
        key->step_id == SLURM_BATCH_SCRIPT) {
        free(key);
        return ESPANK_SUCCESS;
    }
    free(key);

    char *str = snccld_format_infos();
    slurm_spank_log(SNCCLD_LOG_PREFIX "info before removal: %s", str);
    slurm_spank_log(SNCCLD_LOG_PREFIX "info count: %lu", infos_count);
    free(str);

    snccld_output_info_t *info = infos[infos_count - 1];
    if (info->tee_pid > 0) {
        int status;
        if (waitpid(info->tee_pid, &status, WNOHANG) == 0) {
            kill(info->tee_pid, SIGKILL);
            waitpid(info->tee_pid, &status, 0);
        }
        info->tee_pid = -1;
    }
    unlink(info->fifo_path);
    free(info);
    infos_count--;

    str = snccld_format_infos();
    slurm_spank_log(SNCCLD_LOG_PREFIX "info after removal: %s", str);
    slurm_spank_log(SNCCLD_LOG_PREFIX "info count: %lu", infos_count);
    free(str);

    return ESPANK_SUCCESS;
}

snccld_output_info_key_t *snccld_new_key() {
    return malloc(sizeof(snccld_output_info_key_t));
}

spank_err_t snccld_get_key_from(spank_t spank, snccld_output_info_key_t *key) {
    if (spank_get_item(spank, S_JOB_ID, &key->job_id) != ESPANK_SUCCESS) {
        slurm_error(SNCCLD_LOG_PREFIX "Failed to get Job ID");
        return ESPANK_ERROR;
    }

    if (spank_get_item(spank, S_JOB_STEPID, &key->step_id) != ESPANK_SUCCESS) {
        slurm_error(SNCCLD_LOG_PREFIX "Failed to get Step ID");
        return ESPANK_ERROR;
    }

    return ESPANK_SUCCESS;
}

snccld_output_info_t *snccld_new_info() {
    return malloc(sizeof(snccld_output_info_t));
}
