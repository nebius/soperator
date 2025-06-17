/// Logging wrappers.

#ifndef SNCCLD_LOG_H
#define SNCCLD_LOG_H

#include <stdbool.h>

#include <slurm/spank.h>

/// Log message prefix.
#define SNCCLD_LOG_PREFIX "SPANK | NCCL DEBUG"

/**
 * Log info message.
 *
 * @param __fmt Format string.
 * @param ... Variadic arguments to fill in the format string.
 */
#define snccld_log_info(__fmt, ...)                                            \
    do {                                                                       \
        slurm_info("%s [I]: " __fmt, SNCCLD_LOG_PREFIX, ##__VA_ARGS__);        \
    } while (false);

/**
 * Log error message.
 *
 * @param __fmt Format string.
 * @param ... Variadic arguments to fill in the format string.
 */
#define snccld_log_error(__fmt, ...)                                           \
    do {                                                                       \
        slurm_error("%s [E]: " __fmt, SNCCLD_LOG_PREFIX, ##__VA_ARGS__);       \
    } while (false);

#ifdef NDEBUG
#define snccld_log_debug(__fmt, ...)
#else // NDEBUG
/**
 * Log debug message.
 * Does nothing in case of release build.
 *
 * @param __fmt Format string.
 * @param ... Variadic arguments to fill in the format string.
 */
#define snccld_log_debug(__fmt, ...)                                           \
    do {                                                                       \
        slurm_debug("%s [D]: " __fmt, SNCCLD_LOG_PREFIX, ##__VA_ARGS__);       \
    } while (false);
#endif // NDEBUG

#endif // SNCCLD_LOG_H
