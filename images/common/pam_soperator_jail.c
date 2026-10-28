#define _GNU_SOURCE
#define PAM_SM_SESSION

/* Enter the shared Soperator jail in a private mount namespace for an SSH session. */

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <sched.h>
#include <security/pam_ext.h>
#include <security/pam_modules.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <syslog.h>
#include <unistd.h>

#ifndef SYS_pivot_root
#error "SYS_pivot_root unavailable on this system"
#endif

struct jail_context {
    pam_handle_t *pamh;
    int container_log_fd;
};

static void jail_log(const struct jail_context *context, const char *format, ...)
{
    char message[512];
    va_list arguments;

    va_start(arguments, format);
    (void)vsnprintf(message, sizeof(message), format, arguments);
    va_end(arguments);

    pam_syslog(context->pamh, LOG_ERR, "%s", message);
    (void)dprintf(
        context->container_log_fd >= 0 ? context->container_log_fd : STDERR_FILENO,
        "pam_soperator_jail: %s\n",
        message
    );
}

static int jail_errno(const struct jail_context *context, const char *operation)
{
    const int saved_errno = errno;

    jail_log(context, "%s: %s", operation, strerror(saved_errno));
    return PAM_SESSION_ERR;
}

static int validate_root_owned_path(const struct jail_context *context, const char *path)
{
    char resolved[PATH_MAX];
    char component[PATH_MAX];
    const char *cursor;
    size_t component_length = 1;

    if (path == NULL || path[0] != '/' || strcmp(path, "/") == 0) {
        jail_log(context, "jail path must be an absolute path other than /");
        return PAM_SESSION_ERR;
    }

    if (realpath(path, resolved) == NULL) {
        return jail_errno(context, "resolve jail path");
    }
    if (strcmp(path, resolved) != 0) {
        jail_log(context, "jail path must be canonical: %s", path);
        return PAM_SESSION_ERR;
    }

    component[0] = '/';
    component[1] = '\0';
    cursor = resolved + 1;

    while (*cursor != '\0') {
        const char *separator = strchr(cursor, '/');
        const size_t name_length = separator == NULL ? strlen(cursor) : (size_t)(separator - cursor);
        struct stat status;

        if (component_length > 1) {
            if (component_length + 1 >= sizeof(component)) {
                jail_log(context, "jail path is too long: %s", path);
                return PAM_SESSION_ERR;
            }
            component[component_length++] = '/';
        }
        if (name_length == 0 || component_length + name_length >= sizeof(component)) {
            jail_log(context, "jail path is invalid or too long: %s", path);
            return PAM_SESSION_ERR;
        }

        memcpy(component + component_length, cursor, name_length);
        component_length += name_length;
        component[component_length] = '\0';

        if (lstat(component, &status) != 0) {
            return jail_errno(context, "inspect jail path component");
        }
        if (!S_ISDIR(status.st_mode)) {
            jail_log(context, "path component is not a directory: %s", component);
            return PAM_SESSION_ERR;
        }
        if (status.st_uid != 0 || (status.st_mode & (S_IWGRP | S_IWOTH)) != 0) {
            jail_log(
                context,
                "path component must be root-owned and not group- or world-writable: %s",
                component
            );
            return PAM_SESSION_ERR;
        }

        if (separator == NULL) {
            break;
        }
        cursor = separator + 1;
    }

    return PAM_SUCCESS;
}

static int validate_old_root(
    const struct jail_context *context,
    const char *jail_path,
    char old_root[PATH_MAX]
)
{
    struct stat status;
    const int written = snprintf(old_root, PATH_MAX, "%s/mnt/host", jail_path);

    if (written < 0 || written >= PATH_MAX) {
        jail_log(context, "old-root path is too long");
        return PAM_SESSION_ERR;
    }
    if (lstat(old_root, &status) != 0) {
        return jail_errno(context, "inspect old-root directory");
    }
    if (!S_ISDIR(status.st_mode) || status.st_uid != 0 || (status.st_mode & (S_IWGRP | S_IWOTH)) != 0) {
        jail_log(
            context,
            "old-root path must be a root-owned directory that is not group- or world-writable: %s",
            old_root
        );
        return PAM_SESSION_ERR;
    }

    return PAM_SUCCESS;
}

static int enter_jail(const struct jail_context *context, const char *jail_path)
{
    char old_root[PATH_MAX];
    int result;

    result = validate_root_owned_path(context, jail_path);
    if (result != PAM_SUCCESS) {
        return result;
    }
    result = validate_old_root(context, jail_path, old_root);
    if (result != PAM_SUCCESS) {
        return result;
    }

    if (unshare(CLONE_NEWNS) != 0) {
        return jail_errno(context, "create mount namespace");
    }
    if (mount(NULL, "/", NULL, MS_SLAVE | MS_REC, NULL) != 0) {
        return jail_errno(context, "make root mount recursively slave");
    }

    /* pivot_root requires new_root to be a mount point. The bind mount is
     * private to this session's new mount namespace. */
    if (mount(jail_path, jail_path, NULL, MS_BIND | MS_REC, NULL) != 0) {
        return jail_errno(context, "bind-mount jail root");
    }
    if (syscall(SYS_pivot_root, jail_path, old_root) != 0) {
        return jail_errno(context, "pivot into jail root");
    }
    if (chdir("/") != 0) {
        return jail_errno(context, "change directory to jail root");
    }
    if (umount2("/mnt/host", MNT_DETACH) != 0) {
        return jail_errno(context, "detach old root");
    }

    /* Recreate proc in the session mount namespace, matching the established
     * Slurm SPANK jail transition. */
    if (mount(NULL, "/proc", NULL, MS_SLAVE | MS_REC, NULL) != 0) {
        return jail_errno(context, "make proc mount recursively slave");
    }
    if (umount2("/proc", MNT_DETACH) != 0) {
        return jail_errno(context, "detach inherited proc mount");
    }
    if (mount("proc", "/proc", "proc", 0, NULL) != 0) {
        return jail_errno(context, "mount proc in jail");
    }

    return PAM_SUCCESS;
}

PAM_EXTERN int pam_sm_open_session(
    pam_handle_t *pamh,
    int flags,
    int argc,
    const char **argv
)
{
    struct jail_context context = {
        .pamh = pamh,
        .container_log_fd = open("/proc/1/fd/2", O_WRONLY | O_CLOEXEC),
    };
    int result;

    (void)flags;

    if (argc != 1 || argv[0] == NULL) {
        jail_log(&context, "expected exactly one argument: <jail-path>");
        result = PAM_SESSION_ERR;
    } else {
        result = enter_jail(&context, argv[0]);
    }

    if (context.container_log_fd >= 0) {
        (void)close(context.container_log_fd);
    }

    return result;
}

PAM_EXTERN int pam_sm_close_session(
    pam_handle_t *pamh,
    int flags,
    int argc,
    const char **argv
)
{
    (void)pamh;
    (void)flags;
    (void)argc;
    (void)argv;

    return PAM_SUCCESS;
}

#ifdef PAM_MODULE_ENTRY
PAM_MODULE_ENTRY("pam_soperator_jail");
#endif
