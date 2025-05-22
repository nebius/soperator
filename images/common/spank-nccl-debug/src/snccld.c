#include "snccld.h"
#include "snccld_args.h"
#include "snccld_mkdir.h"
#include "snccld_state.h"

#include <ctype.h>
#include <fcntl.h>
#include <sched.h>
#include <signal.h>
#include <stdbool.h>
#include <stdint.h>
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

static snccld_state_t *infos[64];
static size_t          infos_count = 0;

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

    snccld_state_key_t *key = snccld_key_new();
    if (snccld_key_get_from(spank, key) != ESPANK_SUCCESS ||
        key->step_id == SLURM_BATCH_SCRIPT) {
        free(key);
        return ESPANK_SUCCESS;
    }

    snccld_state_t *info = snccld_state_new();
    info->key            = *key;
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

    snccld_state_key_t *key = snccld_key_new();
    if (snccld_key_get_from(spank, key) != ESPANK_SUCCESS ||
        key->step_id == SLURM_BATCH_SCRIPT) {
        free(key);
        return ESPANK_SUCCESS;
    }
    free(key);

    char *str = snccld_format_infos();
    slurm_spank_log(SNCCLD_LOG_PREFIX "info before removal: %s", str);
    slurm_spank_log(SNCCLD_LOG_PREFIX "info count: %lu", infos_count);
    free(str);

    snccld_state_t *info = infos[infos_count - 1];
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
