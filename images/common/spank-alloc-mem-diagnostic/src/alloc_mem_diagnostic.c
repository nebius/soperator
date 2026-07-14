#include <errno.h>
#include <inttypes.h>
#include <limits.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#include <slurm/slurm.h>
#include <slurm/spank.h>

SPANK_PLUGIN("alloc_mem_diagnostic", 1);

#define PLUGIN_TAG "alloc_mem_diagnostic"
#define TIMESTAMP_SIZE 32
#define VALUE_SIZE 32

struct sleep_config {
    bool         requested;
    unsigned int seconds;
};

static const char *context_name(spank_context_t context) {
    switch (context) {
        case S_CTX_ERROR:
            return "error";
        case S_CTX_LOCAL:
            return "local";
        case S_CTX_REMOTE:
            return "remote";
        case S_CTX_ALLOCATOR:
            return "allocator";
        case S_CTX_SLURMD:
            return "slurmd";
        case S_CTX_JOB_SCRIPT:
            return "job_script";
        default:
            return "unknown";
    }
}

static void format_timestamp(char *buffer, size_t size) {
    struct timespec now;
    struct tm       utc;
    char            seconds[24];

    if (clock_gettime(CLOCK_REALTIME, &now) != 0 ||
        gmtime_r(&now.tv_sec, &utc) == NULL ||
        strftime(seconds, sizeof(seconds), "%Y-%m-%dT%H:%M:%S", &utc) == 0) {
        snprintf(buffer, size, "unavailable");
        return;
    }

    snprintf(buffer, size, "%s.%03ldZ", seconds, now.tv_nsec / 1000000L);
}

static void format_uint32_item(
    char *buffer, size_t size, spank_err_t rc, uint32_t value
) {
    if (rc == ESPANK_SUCCESS) {
        snprintf(buffer, size, "%" PRIu32, value);
    } else {
        snprintf(buffer, size, "unavailable");
    }
}

static void format_uint64_item(
    char *buffer, size_t size, spank_err_t rc, uint64_t value
) {
    if (rc == ESPANK_SUCCESS) {
        snprintf(buffer, size, "%" PRIu64, value);
    } else {
        snprintf(buffer, size, "unavailable");
    }
}

static const char *error_name(spank_err_t rc) {
    const char *name = spank_strerror(rc);

    return name == NULL ? "unknown" : name;
}

static struct sleep_config parse_sleep_config(int argc, char **argv) {
    struct sleep_config config = {0};

    for (int i = 0; i < argc; ++i) {
        char          *end = NULL;
        unsigned long value;

        if (argv == NULL || argv[i] == NULL ||
            strncmp(argv[i], "sleep=", strlen("sleep=")) != 0) {
            continue;
        }

        config.requested = true;
        errno            = 0;
        value            = strtoul(argv[i] + strlen("sleep="), &end, 10);
        if (errno != 0 || end == argv[i] + strlen("sleep=") || *end != '\0' ||
            value > UINT_MAX) {
            slurm_error(
                PLUGIN_TAG
                " invalid argument '%s'; sleep disabled and plugin will continue",
                argv[i]
            );
            config.seconds = 0;
            continue;
        }

        config.seconds = (unsigned int)value;
    }

    return config;
}

static void sleep_without_failing(unsigned int seconds) {
    struct timespec requested = {
        .tv_sec  = seconds,
        .tv_nsec = 0,
    };
    struct timespec remaining;

    while (nanosleep(&requested, &remaining) != 0) {
        if (errno == EINTR) {
            requested = remaining;
            continue;
        }

        slurm_error(
            PLUGIN_TAG " nanosleep failed: errno=%d (%s); plugin will continue",
            errno,
            strerror(errno)
        );
        return;
    }
}

int slurm_spank_init(spank_t spank, int argc, char **argv) {
    spank_context_t context = spank_context();

    if (context != S_CTX_REMOTE) {
        return ESPANK_SUCCESS;
    }

    uint32_t job_id      = 0;
    uint32_t step_id     = 0;
    uint32_t node_id     = 0;
    uint64_t job_mem_mb  = 0;
    uint64_t step_mem_mb = 0;

    spank_err_t job_id_rc = spank_get_item(spank, S_JOB_ID, &job_id);
    spank_err_t step_id_rc =
        spank_get_item(spank, S_JOB_STEPID, &step_id);
    spank_err_t node_id_rc =
        spank_get_item(spank, S_JOB_NODEID, &node_id);
    spank_err_t job_mem_rc =
        spank_get_item(spank, S_JOB_ALLOC_MEM, &job_mem_mb);
    spank_err_t step_mem_rc =
        spank_get_item(spank, S_STEP_ALLOC_MEM, &step_mem_mb);

    char job_id_value[VALUE_SIZE];
    char step_id_value[VALUE_SIZE];
    char node_id_value[VALUE_SIZE];
    char job_mem_value[VALUE_SIZE];
    char step_mem_value[VALUE_SIZE];
    char timestamp[TIMESTAMP_SIZE];

    format_uint32_item(job_id_value, sizeof(job_id_value), job_id_rc, job_id);
    format_uint32_item(
        step_id_value, sizeof(step_id_value), step_id_rc, step_id
    );
    format_uint32_item(
        node_id_value, sizeof(node_id_value), node_id_rc, node_id
    );
    format_uint64_item(
        job_mem_value, sizeof(job_mem_value), job_mem_rc, job_mem_mb
    );
    format_uint64_item(
        step_mem_value, sizeof(step_mem_value), step_mem_rc, step_mem_mb
    );
    format_timestamp(timestamp, sizeof(timestamp));

    struct sleep_config sleep_config = parse_sleep_config(argc, argv);

    slurm_info(
        PLUGIN_TAG
        " event=init timestamp=%s pid=%ld context=%s context_id=%d "
        "job_id=%s job_id_rc=%d(%s) "
        "step_id=%s step_id_rc=%d(%s) "
        "node_id=%s node_id_rc=%d(%s) "
        "job_alloc_mem_mb=%s job_alloc_mem_rc=%d(%s) "
        "step_alloc_mem_mb=%s step_alloc_mem_rc=%d(%s) "
        "sleep_requested=%s sleep_seconds=%u",
        timestamp,
        (long)getpid(),
        context_name(context),
        (int)context,
        job_id_value,
        (int)job_id_rc,
        error_name(job_id_rc),
        step_id_value,
        (int)step_id_rc,
        error_name(step_id_rc),
        node_id_value,
        (int)node_id_rc,
        error_name(node_id_rc),
        job_mem_value,
        (int)job_mem_rc,
        error_name(job_mem_rc),
        step_mem_value,
        (int)step_mem_rc,
        error_name(step_mem_rc),
        sleep_config.requested ? "true" : "false",
        sleep_config.seconds
    );

    if (sleep_config.seconds > 0) {
        sleep_without_failing(sleep_config.seconds);
        format_timestamp(timestamp, sizeof(timestamp));
        slurm_info(
            PLUGIN_TAG
            " event=sleep_complete timestamp=%s pid=%ld context=%s "
            "job_id=%s step_id=%s sleep_seconds=%u",
            timestamp,
            (long)getpid(),
            context_name(context),
            job_id_value,
            step_id_value,
            sleep_config.seconds
        );
    }

    return ESPANK_SUCCESS;
}
