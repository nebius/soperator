#include "snccld.h"
#include "snccld_args.h"
#include "snccld_state.h"
#include "snccld_util_antidupl.h"
#include "snccld_util_dir.h"

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
        "%s: %s\t%s\t%d\t%s\t%d\t%s\t%u\t%u\t%d",
        SNCCLD_LOG_PREFIX,
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

static spank_err_t snccld_prepare_directories() {
    slurm_spank_log(
        "%s: Prepare directories: '%s', '%s'.",
        SNCCLD_LOG_PREFIX,
        snccld_config.out_dir,
        SNCCLD_DEFAULT_DIR
    );

    if (snccld_mkdir_p(snccld_config.out_dir, SNCCLD_DEFAULT_MODE) ==
        ESPANK_ERROR) {
        slurm_error(
            "%s: Cannot create directory %s: %m",
            SNCCLD_LOG_PREFIX,
            snccld_config.out_dir
        );
        return ESPANK_ERROR;
    }

    if (snccld_mkdir_p(SNCCLD_DEFAULT_DIR, SNCCLD_DEFAULT_MODE) ==
        ESPANK_ERROR) {
        slurm_error(
            "%s: Cannot create directory '%s': %m",
            SNCCLD_LOG_PREFIX,
            SNCCLD_DEFAULT_DIR
        );
        return ESPANK_ERROR;
    }

    return ESPANK_SUCCESS;
}

static void snccld_run_named_pipe_reading_process(
    snccld_state_t *state, const bool user_set_debug_file, char *user_debug_file
) {
    // Build shell command: <SHELL> -c '<COMMAND>'
    char *sh_argv[4];
    int   sh_idx = 0;

    // Choose shell.
    {
        sh_argv[sh_idx++] =
            (access("/bin/bash", X_OK) == 0) ? "/bin/bash" : "/bin/sh";
        sh_argv[sh_idx++] = "-c";
    }

    // Build tee command: <STDBUF> -oL <TEE> -a [TARGETS]
    char *tee_argv[11];
    int   tee_idx = 0;

    // Use line buffering instead of tee's internal buffer.
    tee_argv[tee_idx++] = "/usr/bin/stdbuf";
    tee_argv[tee_idx++] = "-oL";

    // Run tee to append to files.
    tee_argv[tee_idx++] = "/usr/bin/tee";
    tee_argv[tee_idx++] = "-a";

    // Construct a list of unique targets.
    char *output_targets[2];
    {
        size_t target_count = 0;

        // Include user-specified debug file.
        if (user_set_debug_file) {
            output_targets[target_count++] = user_debug_file;
        }

        // Include out file.
        if (snccld_config.out_file) {
            output_targets[target_count++] = state->log_path;
        }

        size_t unique_output_targets =
            snccld_remove_string_duplicates(output_targets, target_count);
        for (size_t i = 0; i < unique_output_targets; ++i) {
            tee_argv[tee_idx++] = output_targets[i];
        }
    }

    // Take input from named pipe.
    tee_argv[tee_idx++] = "<";
    tee_argv[tee_idx++] = state->fifo_path;

    // Suppress stdout.
    if (!snccld_config.out_stdout) {
        tee_argv[tee_idx++] = ">";
        tee_argv[tee_idx++] = "/dev/null";
    }

    {
        size_t buf_size    = PATH_MAX * 3;
        char  *tee_command = malloc(buf_size);
        size_t offset      = 0;
        for (size_t i = 0; i < tee_idx; ++i) {
            offset += snprintf(
                tee_command + offset, buf_size - offset, "%s ", tee_argv[i]
            );
        }
        sh_argv[sh_idx++] = tee_command;
    }
    sh_argv[sh_idx] = NULL;

    slurm_spank_log("Running: %s -c '%s'", sh_argv[0], sh_argv[2]);

    execvp(sh_argv[0], sh_argv);

    _exit(EXIT_FAILURE);
}

int slurm_spank_user_init(spank_t spank, int argc, char **argv) {
    log_context("user_init", spank);

    if (spank_context() != S_CTX_REMOTE) {
        return ESPANK_SUCCESS;
    }

    slurm_spank_log(
        "%s: Config:\n"
        "\t" SNCCLD_ARG_ENABLED ": %s\n"
        "\t" SNCCLD_ARG_LOG_LEVEL ": %s\n"
        "\t" SNCCLD_ARG_OUT_DIR ": %s\n"
        "\t" SNCCLD_ARG_OUT_FILE ": %s\n"
        "\t" SNCCLD_ARG_OUT_STDOUT ": %s",
        SNCCLD_LOG_PREFIX,
        snccld_config.enabled ? "true" : "false",
        snccld_config.log_level,
        snccld_config.out_dir,
        snccld_config.out_file ? "true" : "false",
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

    // Set forced debug level.
    slurm_spank_log(
        "%s: Setting %s=%s",
        SNCCLD_LOG_PREFIX,
        SNCCLD_NCCL_ENV_DEBUG,
        snccld_config.log_level
    );
    spank_setenv(spank, SNCCLD_NCCL_ENV_DEBUG, snccld_config.log_level, 1);

    // Check if user set debug file.
    char user_debug_file[PATH_MAX] = "";
    bool user_set_debug_file =
        (spank_getenv(
             spank,
             SNCCLD_NCCL_ENV_DEBUG_FILE,
             user_debug_file,
             sizeof(user_debug_file)
         ) == ESPANK_SUCCESS);
    slurm_spank_log(
        "%s: user_set_debug_file =%u", SNCCLD_LOG_PREFIX, user_set_debug_file
    );

    // Neither outfile nor stdout requested nor user set debug file -> noop
    if (!snccld_config.out_file && !snccld_config.out_stdout &&
        !user_set_debug_file) {
        slurm_spank_log(
            "%s: Neither out file nor stdout requested nor user set debug "
            "file. "
            "Skipping.",
            SNCCLD_LOG_PREFIX
        );
        free(key);
        free(state);
        return ESPANK_SUCCESS;
    }

    // Ensure necessary directories exist.
    if (snccld_config.out_file || snccld_config.out_stdout ||
        user_set_debug_file) {
        if (snccld_prepare_directories() != ESPANK_SUCCESS) {
            free(key);
            free(state);
            return ESPANK_ERROR;
        }
    }

    if (snccld_config.out_file) {
        snprintf(
            state->log_path,
            sizeof(state->log_path),
            SNCCLD_TEMPLATE_FILE_NAME,
            snccld_config.out_dir,
            key->job_id,
            key->step_id,
            "out"
        );
    }

    // Only out file needed -> set NCCL_DEBUG_FILE.
    if (snccld_config.out_file && !snccld_config.out_stdout &&
        !user_set_debug_file) {
        slurm_spank_log("%s: Only out file needed.", SNCCLD_LOG_PREFIX);
        slurm_spank_log(
            "%s: Setting %s=%s",
            SNCCLD_LOG_PREFIX,
            SNCCLD_NCCL_ENV_DEBUG_FILE,
            state->log_path
        );
        spank_setenv(spank, SNCCLD_NCCL_ENV_DEBUG_FILE, state->log_path, 1);

        free(key);
        free(state);

        return ESPANK_SUCCESS;
    }

    // If we're here, named pipe has to be constructed.

    slurm_spank_log("%s: Named pipe has to be constructed.", SNCCLD_LOG_PREFIX);
    snprintf(
        state->fifo_path,
        sizeof(state->fifo_path),
        SNCCLD_TEMPLATE_FILE_NAME,
        SNCCLD_DEFAULT_DIR,
        key->job_id,
        key->step_id,
        "fifo"
    );
    if (mkfifo(state->fifo_path, SNCCLD_DEFAULT_MODE) != 0 && errno != EEXIST) {
        slurm_error(
            "%s: Cannot create named pipe '%s': %m",
            SNCCLD_LOG_PREFIX,
            state->fifo_path
        );

        free(key);
        free(state);

        return ESPANK_ERROR;
    }

    if (snccld_state_write(key, state) != ESPANK_SUCCESS) {
        free(key);
        free(state);
        return ESPANK_ERROR;
    }

    char *str = snccld_state_to_string(state);
    slurm_spank_log("%s: State: \n%s", SNCCLD_LOG_PREFIX, str);
    free(str);

    free(key);
    free(state);

    return ESPANK_SUCCESS;
}

int slurm_spank_task_init(spank_t spank, int argc, char **argv) {
    log_context("task_init", spank);

    if (spank_context() != S_CTX_REMOTE) {
        return ESPANK_SUCCESS;
    }

    slurm_spank_log(
        "%s: Config:\n"
        "\t" SNCCLD_ARG_ENABLED ": %s\n"
        "\t" SNCCLD_ARG_LOG_LEVEL ": %s\n"
        "\t" SNCCLD_ARG_OUT_DIR ": %s\n"
        "\t" SNCCLD_ARG_OUT_FILE ": %s\n"
        "\t" SNCCLD_ARG_OUT_STDOUT ": %s",
        SNCCLD_LOG_PREFIX,
        snccld_config.enabled ? "true" : "false",
        snccld_config.log_level,
        snccld_config.out_dir,
        snccld_config.out_file ? "true" : "false",
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
        return ESPANK_ERROR;
    }

    char *str = snccld_state_to_string(state);
    slurm_spank_log("%s: State: \n%s", SNCCLD_LOG_PREFIX, str);
    free(str);

    // Forking fan-out process is not needed
    // if named pipe is not created,
    // or it's already forked.
    slurm_spank_log(
        "%s: FIFO path len: %lu", SNCCLD_LOG_PREFIX, strlen(state->fifo_path)
    );
    slurm_spank_log("%s: Tee PID: %d", SNCCLD_LOG_PREFIX, state->tee_pid);
    if (strlen(state->fifo_path) <= 0 || state->tee_pid > 0) {
        slurm_spank_log(
            "%s: Forking fan-out process is not needed.", SNCCLD_LOG_PREFIX
        );
        free(key);
        free(state);
        return ESPANK_SUCCESS;
    }

    char user_debug_file[PATH_MAX] = "";
    bool user_set_debug_file =
        (spank_getenv(
             spank,
             SNCCLD_NCCL_ENV_DEBUG_FILE,
             user_debug_file,
             sizeof(user_debug_file)
         ) == ESPANK_SUCCESS);
    slurm_spank_log(
        "%s: user_set_debug_file =%u", SNCCLD_LOG_PREFIX, user_set_debug_file
    );

    // Create separate process to fan out logs from the named pipe.
    const pid_t tee_pid = fork();
    if (tee_pid < 0) {
        // Forking failed.
        slurm_error(
            "%s: Cannot create named pipe reading process: %m",
            SNCCLD_LOG_PREFIX
        );
        free(key);
        free(state);
        return ESPANK_ERROR;
    } else if (tee_pid == 0) {
        // We're in forked process.
        snccld_run_named_pipe_reading_process(
            state, user_set_debug_file, user_debug_file
        );
    } else {
        // We're in main process -> tee_pid is a pid of the forked process.
        state->tee_pid = tee_pid;
    }
    slurm_spank_log("%s: Tee PID: %d", SNCCLD_LOG_PREFIX, state->tee_pid);

    slurm_spank_log(
        "%s: Setting %s=%s",
        SNCCLD_LOG_PREFIX,
        SNCCLD_NCCL_ENV_DEBUG_FILE,
        state->fifo_path
    );
    spank_setenv(spank, SNCCLD_NCCL_ENV_DEBUG_FILE, state->fifo_path, 1);

    if (snccld_state_write(key, state) != ESPANK_SUCCESS) {
        free(key);
        free(state);
        return ESPANK_ERROR;
    }

    str = snccld_state_to_string(state);
    slurm_spank_log("%s: State: \n%s", SNCCLD_LOG_PREFIX, str);
    free(str);

    free(key);
    free(state);

    return ESPANK_SUCCESS;
}

int slurm_spank_task_exit(spank_t spank, int argc, char **argv) {
    log_context("task_exit", spank);

    if (spank_context() != S_CTX_REMOTE) {
        return ESPANK_SUCCESS;
    }

    slurm_spank_log(
        "%s: Config:\n"
        "\t" SNCCLD_ARG_ENABLED ": %s\n"
        "\t" SNCCLD_ARG_LOG_LEVEL ": %s\n"
        "\t" SNCCLD_ARG_OUT_DIR ": %s\n"
        "\t" SNCCLD_ARG_OUT_FILE ": %s\n"
        "\t" SNCCLD_ARG_OUT_STDOUT ": %s",
        SNCCLD_LOG_PREFIX,
        snccld_config.enabled ? "true" : "false",
        snccld_config.log_level,
        snccld_config.out_dir,
        snccld_config.out_file ? "true" : "false",
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
        return ESPANK_ERROR;
    }

    char *str = snccld_state_to_string(state);
    slurm_spank_log("%s: State: \n%s", SNCCLD_LOG_PREFIX, str);
    free(str);

    // Kill fan-out process if exists.
    if (state->tee_pid > 0) {
        slurm_spank_log(
            "%s: Killing named pipe reading process with pid %d.",
            SNCCLD_LOG_PREFIX,
            state->tee_pid
        );
        int status;
        if (waitpid(state->tee_pid, &status, WNOHANG) == 0) {
            kill(state->tee_pid, SIGKILL);
            waitpid(state->tee_pid, &status, 0);
        }
        state->tee_pid = -1;
    }

    // Remove named pipe if exists.
    if (strlen(state->fifo_path) > 0) {
        slurm_spank_log(
            "%s: Removing named pipe '%s'.", SNCCLD_LOG_PREFIX, state->fifo_path
        );
        unlink(state->fifo_path);
    } else {
        slurm_spank_log("%s: No named pipe to remove.", SNCCLD_LOG_PREFIX);
    }

    str = snccld_state_to_string(state);
    slurm_spank_log("%s: state: \n%s", SNCCLD_LOG_PREFIX, str);
    free(str);

    snccld_state_cleanup(key);

    free(key);
    free(state);

    return ESPANK_SUCCESS;
}
