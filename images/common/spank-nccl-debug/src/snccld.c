#include "snccld.h"

#include <sched.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include <sys/mount.h>
#include <sys/stat.h>

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
    char context[16];
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

    pid_t pid = getpid();
    pid_t ppid = getppid();
    char *pname = get_executable_name(pid);
    char *parent_name = get_executable_name(ppid);

    uint32_t job_id = 0, job_stepid = 0;
    pid_t task_pid = 0;

    spank_get_item(spank, S_JOB_ID, &job_id);
    spank_get_item(spank, S_JOB_STEPID, &job_stepid);
    spank_get_item(spank, S_TASK_PID, &task_pid);

    slurm_spank_log(
        SNCCLDEBUG_LOG_PREFIX
        "%s\t%s\t%d\t%s\t%d\t%s\t%u\t%u\t%d",
        func_name, context, pid, pname, ppid, parent_name, job_id, job_stepid, task_pid
    );
}

static snccld_output_info_t* infos[64];
static size_t infos_count = 0;

char *snccld_format_infos() {
    if (infos_count == 0 || infos[0] == NULL) {
        return strdup("[]");
    }

    size_t buf_size = 64 * infos_count + 3;
    char *result = malloc(buf_size);
    if (!result) return NULL;


    size_t offset = 0;
    offset += snprintf(result + offset, buf_size - offset, "[");

    for (size_t i = 0; i < infos_count; ++i) {
        offset += snprintf(
            result + offset, buf_size - offset,
            "(job=%u, step=%u, tee=%u, pipe=%s)%s",
            infos[i]->job_id, infos[i]->step_id, infos[i]->tee_pid, infos[i]->pipe_name,
            (i < infos_count - 1) ? ", " : ""
        );
    }

    snprintf(result + offset, buf_size - offset, "]");
    return result;
}

int slurm_spank_init(spank_t sp, int ac, char **av) {
    log_context("init", sp);
    srand(1);
    return ESPANK_SUCCESS;
}

int slurm_spank_user_init(spank_t sp, int ac, char **av) {
    log_context("user_init", sp);

    snccld_output_info_t *info = snccld_output_info_get_from(sp);

    if (info->step_id == SLURM_BATCH_SCRIPT) {
        return ESPANK_SUCCESS;
    }

    info->tee_pid = (rand() * INT32_MAX / 2) % INT32_MAX;
    sprintf(info->pipe_name, "pipe_%u_%u", info->job_id, info->step_id);
    infos[infos_count++] = info;

    char *str = snccld_format_infos();
    slurm_spank_log(SNCCLDEBUG_LOG_PREFIX "added new info: %s", str);
    slurm_spank_log(SNCCLDEBUG_LOG_PREFIX "info count: %lu", infos_count);
    free(str);

    return ESPANK_SUCCESS;
}

int slurm_spank_task_exit(spank_t sp, int argc, char **argv) {
    log_context("task_exit", sp);

    snccld_output_info_t *info = snccld_output_info_get_from(sp);
    if (info->step_id == SLURM_BATCH_SCRIPT) {
        free(info);
        return ESPANK_SUCCESS;
    }
    free(info);

    char *str = snccld_format_infos();
    slurm_spank_log(SNCCLDEBUG_LOG_PREFIX "info before removal: %s", str);
    slurm_spank_log(SNCCLDEBUG_LOG_PREFIX "info count: %lu", infos_count);
    free(str);

    free(infos[--infos_count]);

    str = snccld_format_infos();
    slurm_spank_log(SNCCLDEBUG_LOG_PREFIX "info after removal: %s", str);
    slurm_spank_log(SNCCLDEBUG_LOG_PREFIX "info count: %lu", infos_count);
    free(str);

    return ESPANK_SUCCESS;
}
