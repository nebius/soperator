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

    snccld_state_t *state = snccld_state_new();

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
        free(key);
        free(state);
        return ESPANK_ERROR;
    }
    snprintf(
        state->fifo_path,
        sizeof(state->fifo_path),
        "%s/%u_%u.fifo",
        SNCCLD_DEFAULT_DIR,
        key->job_id,
        key->step_id
    );

    if (snccld_mkdir_p(snccld_config.out_dir, SNCCLD_DEFAULT_FIFO_MODE) ==
        ESPANK_ERROR) {
        slurm_error(
            SNCCLD_LOG_PREFIX "Cannot create directory '%s': %m",
            snccld_config.out_dir
        );
        free(key);
        free(state);
        return ESPANK_ERROR;
    }
    snprintf(
        state->log_path,
        sizeof(state->log_path),
        "%s/%u_%u.out",
        snccld_config.out_dir,
        key->job_id,
        key->step_id
    );

    if (mkfifo(state->fifo_path, SNCCLD_DEFAULT_FIFO_MODE) != EXIT_SUCCESS) {
        if (errno == EEXIST) {
            unlink(state->fifo_path);
            if (mkfifo(state->fifo_path, SNCCLD_DEFAULT_FIFO_MODE) !=
                EXIT_SUCCESS) {
                slurm_error(
                    SNCCLD_LOG_PREFIX "Cannot create FIFO %s: %m",
                    state->fifo_path
                );
                free(key);
                free(state);
                return ESPANK_SUCCESS;
            }
        } else {
            slurm_error(
                SNCCLD_LOG_PREFIX "Cannot create FIFO %s: %m", state->fifo_path
            );
            free(key);
            free(state);
            return ESPANK_SUCCESS;
        }
    }

    pid_t pid = fork();
    if (pid < 0) {
        slurm_error(SNCCLD_LOG_PREFIX "fork() failed: %m");
        unlink(state->fifo_path);
        return ESPANK_SUCCESS;
    } else if (pid == 0) {
        int fd = open(state->fifo_path, O_RDONLY);
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
            state->log_path,
            (char *)NULL
        );
        _exit(EXIT_FAILURE);
    }

    state->tee_pid = pid;
    if (snccld_state_write(key, state) != ESPANK_SUCCESS) {
        free(key);
        free(state);
        return ESPANK_ERROR;
    }

    char *str = snccld_state_to_string(state);
    slurm_spank_log(SNCCLD_LOG_PREFIX "state: %s", str);
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
            state->fifo_path
        );
        spank_setenv(spank, SNCCLD_NCCL_ENV_DEBUG_FILE, state->fifo_path, 1);
    }

    free(key);
    free(state);

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

    snccld_state_t *state = snccld_state_read(key);
    if (state == NULL) {
        free(key);
        free(state);
        return ESPANK_ERROR;
    }

    char *str = snccld_state_to_string(state);
    slurm_spank_log(SNCCLD_LOG_PREFIX "state: %s", str);
    free(str);

    if (state->tee_pid > 0) {
        int status;
        if (waitpid(state->tee_pid, &status, WNOHANG) == 0) {
            kill(state->tee_pid, SIGKILL);
            waitpid(state->tee_pid, &status, 0);
        }
        state->tee_pid = -1;
    }
    unlink(state->fifo_path);

    str = snccld_state_to_string(state);
    slurm_spank_log(SNCCLD_LOG_PREFIX "state: %s", str);
    free(str);

    snccld_state_cleanup(key);

    free(key);
    free(state);

    return ESPANK_SUCCESS;
}
