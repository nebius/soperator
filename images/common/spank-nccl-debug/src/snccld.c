#include "snccld.h"

#include "snccld_args.h"
#include "snccld_enroot.h"
#include "snccld_log.h"
#include "snccld_nccl.h"
#include "snccld_state.h"
#include "snccld_util_dir_file.h"
#include "snccld_util_host.h"
#include "snccld_util_oplock.h"
#include "snccld_util_string.h"

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

XSPANK_PLUGIN(SNCCLD_PLUGIN_NAME, 1);

#ifndef NDEBUG
char *_snccld_get_executable_name(pid_t pid) {
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
#endif

/**
 * Substitute %h placeholder with hostname in directory path.
 *
 * @param path The path potentially containing %h placeholder.
 * @param hostname The hostname to substitute for %h.
 * @param output Buffer to store the result (must be PATH_MAX + 1 bytes).
 */
static void snccld_substitute_hostname(
    const char *path, const char *hostname, char *output
) {
    const char *p   = path;
    char       *out = output;

    while (*p && (out - output) < PATH_MAX) {
        if (*p == '%' && *(p + 1) == 'h') {
            // Replace %h with hostname
            out += snprintf(out, PATH_MAX - (out - output), "%s", hostname);
            p   += 2; // Skip %h
        } else {
            *out++ = *p++;
        }
    }
    *out = '\0';
}

void _snccld_log_context(const char *func_name, spank_t spank) {
#ifdef NDEBUG
    return;
#else
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
    char *pname       = _snccld_get_executable_name(pid);
    char *parent_name = _snccld_get_executable_name(ppid);

    uint32_t job_id = 0, job_stepid = 0;
    pid_t    task_pid = 0;

    spank_get_item(spank, S_JOB_ID, &job_id);
    spank_get_item(spank, S_JOB_STEPID, &job_stepid);
    spank_get_item(spank, S_TASK_PID, &task_pid);

    snccld_log_debug(
        "%s\t%s\t%d\t%s\t%d\t%s\t%u\t%u\t%d",
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
#endif
}

int slurm_spank_init(spank_t spank, int argc, char **argv) {
    _snccld_log_context("slurm_spank_init", spank);

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

static void snccld_run_named_pipe_reading_process(snccld_state_t *state) {
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
        if (strlen(state->user_log_path) > 0) {
            output_targets[target_count++] = state->user_log_path;
        }

        // Include out file.
        if (snccld_config.out_file) {
            output_targets[target_count++] = state->log_path;
        }

        const size_t unique_output_targets =
            snccld_remove_string_duplicates(output_targets, target_count);
        for (size_t i = 0; i < unique_output_targets; ++i) {
            tee_argv[tee_idx++] = output_targets[i];
        }
        // Remove duplicate elements.
        for (size_t i = unique_output_targets; i < target_count; ++i) {
            free(output_targets[i]);
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
        size_t buf_size    = PATH_MAX * tee_idx + (tee_idx - 1) + 1;
        char  *tee_command = malloc(buf_size);
        size_t offset      = 0;
        for (size_t i = 0; i < tee_idx; ++i) {
            size_t w = snprintf(
                tee_command + offset, buf_size - offset, "%s ", tee_argv[i]
            );
            if (offset + w < buf_size) {
                offset += w;
            }
        }
        sh_argv[sh_idx++] = tee_command;
    }
    sh_argv[sh_idx] = NULL;

    snccld_log_debug("Running: %s -c '%s'", sh_argv[0], sh_argv[2]);

    execvp(sh_argv[0], sh_argv);

    _exit(EXIT_FAILURE);
}

int slurm_spank_user_init(spank_t spank, int argc, char **argv) {
    _snccld_log_context("slurm_spank_user_init", spank);

    if (spank_context() != S_CTX_REMOTE) {
        return ESPANK_SUCCESS;
    }

    snccld_log_debug(
        "Config:\n"
        "\t" SNCCLD_ARG_ENABLED ": %s\n"
        "\t" SNCCLD_ARG_LOG_LEVEL ": %s\n"
        "\t" SNCCLD_ARG_OUT_DIR ": %s\n"
        "\t" SNCCLD_ARG_OUT_FILE ": %s\n"
        "\t" SNCCLD_ARG_OUT_STDOUT ": %s",
        snccld_config.enabled ? "true" : "false",
        snccld_config.log_level,
        snccld_config.out_dir,
        snccld_config.out_file ? "true" : "false",
        snccld_config.out_stdout ? "true" : "false"
    );

    if (!snccld_config.enabled) {
        return ESPANK_SUCCESS;
    }

    char *hostname = snccld_get_hostname();
    snccld_log_debug("hostname=%s", hostname);

    snccld_state_key_t *key = snccld_key_new();
    if (snccld_key_get_from(spank, key) != ESPANK_SUCCESS ||
        key->step_id == SLURM_BATCH_SCRIPT) {
        goto user_init_exit;
    }

    // Ensure `user_init` ran once per worker.
    snccld_ensure_dir_exists(SNCCLD_SYSTEM_DIR, false);
    if (!snccld_acquire_lock(
            key->job_id, key->step_id, SNCCLD_OPLOCK_OP_USER_INIT, hostname
        )) {
        goto user_init_exit;
    }

    {
        char       user_debug[8] = "";
        const bool user_set_debug =
            (spank_getenv(
                 spank, SNCCLD_NCCL_ENV_DEBUG, user_debug, sizeof(user_debug)
             ) == ESPANK_SUCCESS &&
             strlen(user_debug) > 0);
        snccld_log_debug("user_set_debug=%u", user_set_debug);
        if (user_set_debug) {
            snccld_log_info(
                "Enabling output to stdout as user set %s on their own.",
                SNCCLD_NCCL_ENV_DEBUG
            );
            snccld_config.out_stdout = true;
        }
    }

    // Set forced debug level.
    snccld_log_info(
        "Setting %s=%s", SNCCLD_NCCL_ENV_DEBUG, snccld_config.log_level
    );
    spank_setenv(spank, SNCCLD_NCCL_ENV_DEBUG, snccld_config.log_level, 1);

    // Check if user set debug file.
    char       user_debug_file[PATH_MAX + 1] = "";
    const bool user_set_debug_file =
        (spank_getenv(
             spank,
             SNCCLD_NCCL_ENV_DEBUG_FILE,
             user_debug_file,
             sizeof(user_debug_file)
         ) == ESPANK_SUCCESS &&
         strlen(user_debug_file) > 0);
    snccld_log_debug("user_set_debug_file=%u", user_set_debug_file);

    // Neither outfile nor stdout requested nor user set debug file -> noop
    if (!snccld_config.out_file && !snccld_config.out_stdout &&
        !user_set_debug_file) {
        snccld_log_info(
            "Neither out file nor stdout requested nor user set debug file. "
            "Skipping."
        );
        goto user_init_exit;
    }

    snccld_state_t *state = snccld_state_new();

    if (snccld_config.out_file) {
        char resolved_out_dir[PATH_MAX + 1];
        snccld_substitute_hostname(
            snccld_config.out_dir, hostname, resolved_out_dir
        );

        snprintf(
            state->log_path,
            sizeof(state->log_path),
            "%s/%s.%u.%u.out",
            resolved_out_dir,
            hostname,
            key->job_id,
            key->step_id
        );
    }
    if (user_set_debug_file) {
        snprintf(
            state->user_log_path,
            sizeof(state->user_log_path),
            "%s",
            user_debug_file
        );
    }

    // Check if only 'NCCL_ENV_DEBUG_FILE' has to be set.
    {
        const bool only_out_file =
            snccld_config.out_file &&
            !(snccld_config.out_stdout || user_set_debug_file);
        const bool only_user_file =
            user_set_debug_file &&
            !(snccld_config.out_file || snccld_config.out_stdout);
        const bool only_stdout =
            snccld_config.out_stdout &&
            !(user_set_debug_file || snccld_config.out_file);

        if (!(only_out_file || only_user_file || only_stdout)) {
            goto user_init_create_fifo;
        }

        snccld_log_info("Only %s has to be set.", SNCCLD_NCCL_ENV_DEBUG_FILE);

        char *out_file = NULL;
        if (only_out_file) {
            out_file = strdup(state->log_path);
        } else if (only_user_file) {
            out_file = strdup(state->user_log_path);
        } else if (only_stdout) {
            out_file = strdup("/dev/stdout");
        }

        snccld_log_info("Setting %s=%s", SNCCLD_NCCL_ENV_DEBUG_FILE, out_file);
        spank_setenv(spank, SNCCLD_NCCL_ENV_DEBUG_FILE, out_file, 1);
        free(out_file);
        goto user_init_write_state;
    }

    // If we're here, FIFO has to be constructed.
user_init_create_fifo:
    snccld_log_info("Named pipe has to be constructed.");
    char fifo_path[PATH_MAX + 1] = "";
    snprintf(
        fifo_path,
        sizeof(fifo_path),
        SNCCLD_TEMPLATE_FILE_NAME,
        SNCCLD_SYSTEM_DIR,
        hostname,
        key->job_id,
        key->step_id,
        "fifo"
    );
    if (mkfifo(fifo_path, SNCCLD_DEFAULT_MODE) != 0 && errno != EEXIST) {
        snccld_log_error("Cannot create named pipe '%s': %m", fifo_path);
        goto user_init_write_state;
    }
    snprintf(state->fifo_path, sizeof(state->fifo_path), "%s", fifo_path);
    snccld_log_info(
        "Setting %s=%s", SNCCLD_NCCL_ENV_DEBUG_FILE, state->fifo_path
    );
    spank_setenv(spank, SNCCLD_NCCL_ENV_DEBUG_FILE, state->fifo_path, 1);

user_init_write_state:
    char *str = snccld_state_to_string(state);
    snccld_log_debug("State: \n%s", str);
    free(str);

    snccld_state_write(key, state, hostname);
    free(state);

user_init_exit:
    free(key);
    free(hostname);
    return ESPANK_SUCCESS;
}

int slurm_spank_task_init_privileged(spank_t spank, int argc, char **argv) {
    _snccld_log_context("slurm_spank_task_init_privileged", spank);

    if (spank_context() != S_CTX_REMOTE) {
        return ESPANK_SUCCESS;
    }

    snccld_log_debug(
        "Config:\n"
        "\t" SNCCLD_ARG_ENABLED ": %s\n"
        "\t" SNCCLD_ARG_LOG_LEVEL ": %s\n"
        "\t" SNCCLD_ARG_OUT_DIR ": %s\n"
        "\t" SNCCLD_ARG_OUT_FILE ": %s\n"
        "\t" SNCCLD_ARG_OUT_STDOUT ": %s",
        snccld_config.enabled ? "true" : "false",
        snccld_config.log_level,
        snccld_config.out_dir,
        snccld_config.out_file ? "true" : "false",
        snccld_config.out_stdout ? "true" : "false"
    );

    if (!snccld_config.enabled) {
        return ESPANK_SUCCESS;
    }

    char *hostname = snccld_get_hostname();
    snccld_log_debug("hostname=%s", hostname);

    snccld_state_key_t *key = snccld_key_new();
    if (snccld_key_get_from(spank, key) != ESPANK_SUCCESS ||
        key->step_id == SLURM_BATCH_SCRIPT) {
        goto task_init_p_exit;
    }

    // Ensure `task_init_privileged` ran once per worker.
    if (!snccld_acquire_lock(
            key->job_id, key->step_id, SNCCLD_OPLOCK_OP_TASK_INIT_P, hostname
        )) {
        goto task_init_p_exit;
    }

    snccld_state_t *state = snccld_state_read(key, hostname);
    if (state == NULL) {
        goto task_init_p_exit;
    }

    if (snccld_config.out_file) {
        char resolved_out_dir[PATH_MAX + 1];
        snccld_substitute_hostname(
            snccld_config.out_dir, hostname, resolved_out_dir
        );
        snccld_log_debug("Ensuring '%s' exists.", resolved_out_dir);
        snccld_ensure_dir_exists(resolved_out_dir, true);
    }
    if (strlen(state->user_log_path) > 0) {
        snccld_log_debug("Ensuring '%s' exists.", state->user_log_path);
        snccld_ensure_file_exists(state->user_log_path, true);
    }

    // Create Enroot bind mounts for the state and log files.
    {
        if (!snccld_dir_exists(SNCCLD_ENROOT_MOUNT_DIR)) {
            goto mount_config_end;
        }

        char mount_config_filename[PATH_MAX + 1] = "";
        snprintf(
            mount_config_filename,
            sizeof(mount_config_filename),
            "%s/%s-%u-%u.fstab",
            SNCCLD_ENROOT_MOUNT_DIR,
            "30-nccl-debug",
            key->job_id,
            key->step_id
        );
        snccld_log_info(
            "Creating Enroot mount config '%s'.", mount_config_filename
        );

        char lock_filename[PATH_MAX + 1] = "";
        snprintf(
            lock_filename,
            sizeof(lock_filename),
            "%s.lock",
            mount_config_filename
        );

        // Write config once.
        int lock_fd;
        {
            lock_fd =
                open(lock_filename, O_CREAT | O_WRONLY, SNCCLD_DEFAULT_MODE);
            if (lock_fd < 0) {
                snccld_log_error("Cannot open %s: %m", lock_filename);
                goto mount_config_end;
            }
            if (fchmod(lock_fd, SNCCLD_DEFAULT_MODE) != 0) {
                snccld_log_error("Cannot chmod %s: %m", lock_filename);
                close(lock_fd);
                goto mount_config_end;
            }

            if (flock(lock_fd, LOCK_EX | LOCK_NB) == -1) {
                snccld_log_error("Cannot flock %s: %m", lock_filename);
                close(lock_fd);
                goto mount_config_end;
            }
        }

        const int mount_config_fd = open(
            mount_config_filename, O_CREAT | O_WRONLY, SNCCLD_DEFAULT_MODE
        );
        if (mount_config_fd < 0) {
            snccld_log_error("Cannot open %s: %m", mount_config_filename);
            goto mount_config_unflock;
        }
        if (fchmod(mount_config_fd, SNCCLD_DEFAULT_MODE) != 0) {
            snccld_log_error("Cannot chmod %s: %m", mount_config_filename);
            close(mount_config_fd);
            goto mount_config_unflock;
        }

        FILE *mount_config_f = fdopen(mount_config_fd, "w");
        if (mount_config_f == NULL) {
            snccld_log_error("Cannot fdopen %s: %m", mount_config_filename);
            close(mount_config_fd);
            goto mount_config_unflock;
        }

        {
            char  *mounts[2];
            size_t dir_mount_count    = 0;
            mounts[dir_mount_count++] = strdup(SNCCLD_SYSTEM_DIR);
            if (snccld_config.out_file) {
                char resolved_out_dir[PATH_MAX + 1];
                snccld_substitute_hostname(
                    snccld_config.out_dir, hostname, resolved_out_dir
                );
                mounts[dir_mount_count++] = strdup(resolved_out_dir);
            }

            // Write dir mounts to the mount config.
            const size_t unique_dir_mounts =
                snccld_remove_string_duplicates(mounts, dir_mount_count);
            for (size_t i = 0; i < unique_dir_mounts; ++i) {
                fprintf(
                    mount_config_f,
                    SNCCLD_ENROOT_MOUNT_TEMPLATE,
                    mounts[i],
                    mounts[i],
                    SNCCLD_ENROOT_MOUNT_TEMPLATE_DIR
                );
                snccld_log_debug("Created mount for %s", mounts[i]);
                free(mounts[i]);
            }
            // Remove duplicate elements.
            for (size_t i = unique_dir_mounts; i < dir_mount_count; ++i) {
                free(mounts[i]);
            }

            // Don't write user file to the mount config.
            /*
            if (user_set_debug_file) {
                fprintf(
                    mount_config_f,
                    SNCCLD_ENROOT_MOUNT_TEMPLATE,
                    user_debug_file,
                    user_debug_file,
                    SNCCLD_ENROOT_MOUNT_TEMPLATE_FILE
                );
                snccld_log_debug("Created mount for %s", user_debug_file);
            }
            */
        }

        fclose(mount_config_f);
        snprintf(
            state->mounts_path,
            sizeof(state->mounts_path),
            mount_config_filename
        );
        snccld_state_write(key, state, hostname);

    mount_config_unflock:
        flock(lock_fd, LOCK_UN);
        close(lock_fd);
        unlink(lock_filename);

    mount_config_end:
    }

    free(state);

task_init_p_exit:
    free(key);
    free(hostname);
    return ESPANK_SUCCESS;
}

int slurm_spank_task_init(spank_t spank, int argc, char **argv) {
    _snccld_log_context("slurm_spank_task_init", spank);

    if (spank_context() != S_CTX_REMOTE) {
        return ESPANK_SUCCESS;
    }

    snccld_log_debug(
        "Config:\n"
        "\t" SNCCLD_ARG_ENABLED ": %s\n"
        "\t" SNCCLD_ARG_LOG_LEVEL ": %s\n"
        "\t" SNCCLD_ARG_OUT_DIR ": %s\n"
        "\t" SNCCLD_ARG_OUT_FILE ": %s\n"
        "\t" SNCCLD_ARG_OUT_STDOUT ": %s",
        snccld_config.enabled ? "true" : "false",
        snccld_config.log_level,
        snccld_config.out_dir,
        snccld_config.out_file ? "true" : "false",
        snccld_config.out_stdout ? "true" : "false"
    );

    if (!snccld_config.enabled) {
        return ESPANK_SUCCESS;
    }

    char *hostname = snccld_get_hostname();

    snccld_state_key_t *key = snccld_key_new();
    if (snccld_key_get_from(spank, key) != ESPANK_SUCCESS ||
        key->step_id == SLURM_BATCH_SCRIPT) {
        goto task_init_exit;
    }

    // Ensure `task_init` ran once per worker.
    if (!snccld_acquire_lock(
            key->job_id, key->step_id, SNCCLD_OPLOCK_OP_TASK_INIT, hostname
        )) {
        goto task_init_exit;
    }

    snccld_state_t *state = snccld_state_read(key, hostname);
    if (state == NULL) {
        goto task_init_fail;
    }

#ifndef NDEBUG
    char *str = snccld_state_to_string(state);
    snccld_log_debug("State: \n%s", str);
    free(str);
#endif

    // Forking fan-out process is not needed if named pipe is not created,
    // or it's already forked.
    if (strlen(state->fifo_path) <= 0 || state->tee_pid > 0) {
        snccld_log_info("Forking fan-out process is not needed.");
        free(state);
        goto task_init_exit;
    }

    // Create separate process to fan out logs from the fifo.
    const pid_t tee_pid = fork();
    if (tee_pid < 0) {
        // Forking failed.
        snccld_log_error("Cannot create named pipe reading process: %m");
        free(state);
        goto task_init_exit;
    } else if (tee_pid == 0) {
        // We're in forked process.
        snccld_log_info("Forking fan-out process.");
        snccld_run_named_pipe_reading_process(state);
    } else {
        // We're in main process -> tee_pid is a pid of the forked process.
        state->tee_pid = tee_pid;
    }

#ifndef NDEBUG
    str = snccld_state_to_string(state);
    snccld_log_debug("State: \n%s", str);
    free(str);
#endif

    snccld_state_write(key, state, hostname);
    free(state);

task_init_exit:
    free(key);
    free(hostname);
    return ESPANK_SUCCESS;

task_init_fail:
    free(key);
    free(hostname);
    return ESPANK_SUCCESS;
}

int slurm_spank_task_exit(spank_t spank, int argc, char **argv) {
    _snccld_log_context("slurm_spank_task_exit", spank);

    if (spank_context() != S_CTX_REMOTE) {
        return ESPANK_SUCCESS;
    }

    snccld_log_debug(
        "Config:\n"
        "\t" SNCCLD_ARG_ENABLED ": %s\n"
        "\t" SNCCLD_ARG_LOG_LEVEL ": %s\n"
        "\t" SNCCLD_ARG_OUT_DIR ": %s\n"
        "\t" SNCCLD_ARG_OUT_FILE ": %s\n"
        "\t" SNCCLD_ARG_OUT_STDOUT ": %s",
        snccld_config.enabled ? "true" : "false",
        snccld_config.log_level,
        snccld_config.out_dir,
        snccld_config.out_file ? "true" : "false",
        snccld_config.out_stdout ? "true" : "false"
    );

    if (!snccld_config.enabled) {
        return ESPANK_SUCCESS;
    }

    char *hostname = snccld_get_hostname();

    snccld_state_key_t *key = snccld_key_new();
    if (snccld_key_get_from(spank, key) != ESPANK_SUCCESS ||
        key->step_id == SLURM_BATCH_SCRIPT) {
        goto task_exit_exit;
    }

    // Ensure `user_init` ran once per worker.
    if (!snccld_acquire_lock(
            key->job_id, key->step_id, SNCCLD_OPLOCK_OP_TASK_EXIT, hostname
        )) {
        goto task_exit_exit;
    }

    snccld_state_t *state = snccld_state_read(key, hostname);
    if (state == NULL) {
        snccld_release_lock(
            key->job_id, key->step_id, SNCCLD_OPLOCK_OP_TASK_EXIT, hostname
        );
        goto task_exit_fail;
    }

#ifndef NDEBUG
    char *str = snccld_state_to_string(state);
    snccld_log_debug("State: \n%s", str);
    free(str);
#endif

    // Kill fan-out process if exists.
    if (state->tee_pid > 0) {
        snccld_log_info("Killing fan-out process with pid %d.", state->tee_pid);
        int status;
        if (waitpid(state->tee_pid, &status, WNOHANG) == 0) {
            kill(state->tee_pid, SIGKILL);
            waitpid(state->tee_pid, &status, 0);
        }
        state->tee_pid = -1;
    }

    // Remove named pipe if exists.
    if (strlen(state->fifo_path) > 0) {
        snccld_log_info("Removing named pipe '%s'.", state->fifo_path);
        unlink(state->fifo_path);
    } else {
        snccld_log_info("No named pipe to remove.");
    }

    // Remove mount config if created.
    if (strlen(state->mounts_path) > 0) {
        snccld_log_info("Removing mount config '%s'.", state->mounts_path);
        unlink(state->mounts_path);
    } else {
        snccld_log_info("No mount config to remove.");
    }

    free(state);
    snccld_state_cleanup(key, hostname);

    snccld_release_lock(
        key->job_id, key->step_id, SNCCLD_OPLOCK_OP_USER_INIT, hostname
    );
    snccld_release_lock(
        key->job_id, key->step_id, SNCCLD_OPLOCK_OP_TASK_INIT_P, hostname
    );
    snccld_release_lock(
        key->job_id, key->step_id, SNCCLD_OPLOCK_OP_TASK_INIT, hostname
    );
    snccld_release_lock(
        key->job_id, key->step_id, SNCCLD_OPLOCK_OP_TASK_EXIT, hostname
    );

task_exit_exit:
    free(key);
    free(hostname);
    return ESPANK_SUCCESS;

task_exit_fail:
    free(key);
    free(hostname);
    return ESPANK_ERROR;
}
