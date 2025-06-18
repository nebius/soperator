/// Logging wrappers.

#ifndef SNCCLD_LOG_H
#define SNCCLD_LOG_H

#include "snccld_util_host.h"

#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

#include <slurm/spank.h>

/// Log message prefix.
static const char *_SNCCLD_LOG_PREFIX = "SPANK | NCCL DEBUG";

/**
 * Template string for log message prefix.
 *
 * 1. `%s` - Hostname.
 * 2. `%d` - PID.
 * 3. `%s`   - Log prefix.
 * 4. `[%c]` - Log level.
 */
static const char *_SNCCLD_LOG_TEMPLATE_PREFIX = "%s:%d %s [%c]:";

/// Log level enum.
enum _SNCCLD_LOG_LEVEL {
    /// Info log level.
    _SNCCLD_LOG_LEVEL_INFO = 'I',
    /// Error log level.
    _SNCCLD_LOG_LEVEL_ERROR = 'E',
#ifndef NDEBUG
    /// Debug log level.
    _SNCCLD_LOG_LEVEL_DEBUG = 'D'
#endif
};

/**
 * Wrapper around SPANK logging function to add custom prefix.
 *
 * @param __level Log level. @see _SNCCLD_LOG_LEVEL.
 * @param __log_fn SPANK log function.
 * @param __fmt Format string.
 * @param ... Variadic arguments to fill in the format string.
 */
#define _snccld_log_impl(__level, __log_fn, __fmt, ...)                        \
    do {                                                                       \
        char *_hostname   = snccld_get_hostname();                             \
        char  _prefix[64] = "";                                                \
        snprintf(                                                              \
            _prefix,                                                           \
            sizeof(_prefix),                                                   \
            _SNCCLD_LOG_TEMPLATE_PREFIX,                                       \
            _hostname,                                                         \
            getpid(),                                                          \
            _SNCCLD_LOG_PREFIX,                                                \
            __level                                                            \
        );                                                                     \
        free(_hostname);                                                       \
                                                                               \
        char _new_fmt[1024] = "";                                              \
        snprintf(_new_fmt, sizeof(_new_fmt), "%s %s", _prefix, __fmt);         \
        __log_fn(_new_fmt, ##__VA_ARGS__);                                     \
    } while (false);

/**
 * Log info message.
 *
 * @param __fmt Format string.
 * @param ... Variadic arguments to fill in the format string.
 */
#define snccld_log_info(__fmt, ...)                                            \
    _snccld_log_impl(_SNCCLD_LOG_LEVEL_INFO, slurm_info, __fmt, ##__VA_ARGS__)

/**
 * Log error message.
 *
 * @param __fmt Format string.
 * @param ... Variadic arguments to fill in the format string.
 */
#define snccld_log_error(__fmt, ...)                                           \
    _snccld_log_impl(_SNCCLD_LOG_LEVEL_ERROR, slurm_error, __fmt, ##__VA_ARGS__)

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
    _snccld_log_impl(_SNCCLD_LOG_LEVEL_DEBUG, slurm_debug, __fmt, ##__VA_ARGS__)
#endif // NDEBUG

#endif // SNCCLD_LOG_H
