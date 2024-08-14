#define _GNU_SOURCE
#include <slurm/spank.h>
#include <slurm/slurm_errno.h>
#include <stdlib.h>
#include <stdint.h>
#include <sched.h>
#include <stdio.h>
#include <sys/mount.h>
#include <sys/syscall.h>
#include <sys/stat.h>
#include <unistd.h>
#include <errno.h>
#include <string.h>

// pivot_root is not available directly in glibc, so we use syscall
#ifndef SYS_pivot_root
#error "SYS_pivot_root unavailable on this system"
#endif

SPANK_PLUGIN("chroot", 1);

// The following Slurm job step IDs are copied from the Slurm source code:
//
// Max job step ID of normal step
#define SLURM_MAX_NORMAL_STEP_ID (0xfffffff0)
// Job step ID of pending step
#define SLURM_PENDING_STEP (0xfffffffd)
// Job step ID of external process container
#define SLURM_EXTERN_CONT  (0xfffffffc)
// Job step ID of batch scripts
#define SLURM_BATCH_SCRIPT (0xfffffffb)
// Job step ID for the interactive step (if used)
#define SLURM_INTERACTIVE_STEP (0xfffffffa)

int change_root(const char *jail_path) {
    slurm_debug("chroot: change_root: Initialize host_in_jail_path = jail_path + /mnt/host");
    char host_in_jail_path[256];
    host_in_jail_path[0] = '\0'; // Initialize to an empty string
    if (strlen(jail_path) + strlen("/mnt/host") + 1 > sizeof(host_in_jail_path)) {
        fprintf(stderr, "host_in_jail_path buffer is not large enough to hold the concatenated string\n");
        return 10;
    }
    strcat(host_in_jail_path, jail_path);
    strcat(host_in_jail_path, "/mnt/host");

    slurm_debug("chroot: change_root: Create new mount namespace for the current process");
    if (unshare(CLONE_NEWNS) != 0) {
        fprintf(stderr, "unshare --mount: %s\n", strerror(errno));
        return 20;
    }

    slurm_debug("chroot: change_root: Remount old root / as slave");
    if (mount(NULL, "/", NULL, MS_SLAVE | MS_REC, NULL) != 0) {
        fprintf(stderr, "mount --make-rslave /: %s\n", strerror(errno));
        return 30;
    }

    slurm_debug("chroot: change_root: Pivot jail and host roots");
    if (syscall(SYS_pivot_root, jail_path, host_in_jail_path) != 0) {
        fprintf(stderr, "pivot_root ${jail_path} ${jail_path}/mnt/host: %s\n", strerror(errno));
        return 40;
    }

    slurm_debug("chroot: change_root: Unmount old root /mnt/host from jail");
    if (umount2("/mnt/host", MNT_DETACH) != 0) {
        fprintf(stderr, "umount -R /mnt/host: %s\n", strerror(errno));
        return 50;
    }

    slurm_debug("chroot: change_root: Change directory into new root /");
    if (chdir("/") != 0) {
        fprintf(stderr, "chdir /: %s\n", strerror(errno));
        return 60;
    }

    return 0;
}

int remount_proc() {
    slurm_debug("chroot: remount_proc: Remount /proc as slave");
    if (mount(NULL, "/proc", NULL, MS_SLAVE | MS_REC, NULL) != 0) {
        fprintf(stderr, "mount --make-rslave /proc: %s\n", strerror(errno));
        return 10;
    }

    slurm_debug("chroot: remount_proc: Unmount /proc");
    if (umount2("/proc", MNT_DETACH) != 0) {
        fprintf(stderr, "umount -R /proc: %s\n", strerror(errno));
        return 20;
    }

    slurm_debug("chroot: remount_proc: Mount /proc again");
    if (mount("proc", "/proc", "proc", 0, NULL) != 0) {
        fprintf(stderr, "mount -t /proc proc/: %s\n", strerror(errno));
        return 30;
    }

    return 0;
}

int slurm_spank_init_post_opt(spank_t spank, int argc, char **argv) {
    spank_context_t spank_ctx = spank_context();
    if (spank_ctx != S_CTX_REMOTE) {
        slurm_debug("chroot: init_post_opt: Called not in remote context, exit");
        return ESPANK_SUCCESS;
    }

    if (argc != 1) {
        fprintf(stderr, "expected 1 plugin argument: <path_to_jail>, but got %d arguments\n", argc);
        return 100;
    }
    char *jail_path = argv[0];

    uint32_t job_stepid = 0;
    spank_get_item(spank, S_JOB_STEPID, &job_stepid);
    // Possible job step ids:
    // - SLURM_MAX_NORMAL_STEP_ID (any normal job step ID has ID less or equal to that)
    // - SLURM_PENDING_STEP
    // - SLURM_EXTERN_CONT
    // - SLURM_BATCH_SCRIPT
    // - SLURM_INTERACTIVE_STEP
    if (job_stepid <= SLURM_MAX_NORMAL_STEP_ID) {
        slurm_debug("chroot: init_post_opt: Called in normal job step");
    } else if (job_stepid == SLURM_BATCH_SCRIPT) {
        slurm_debug("chroot: init_post_opt: Called in batch job step");
    } else if (job_stepid == SLURM_INTERACTIVE_STEP) {
        slurm_debug("chroot: init_post_opt: Called in interactive job step");
    } else {
        slurm_debug("chroot: init_post_opt: Called not in batch or normal job step, exit");
        return ESPANK_SUCCESS;
    }

    slurm_debug("chroot: init_post_opt: Enter jail environment");

    int res = -1;

    slurm_debug("chroot: init_post_opt: Change the process root into %s", jail_path);
    res = change_root(jail_path);
    if (res != 0) {
        return 200 + res;
    }

    slurm_debug("chroot: init_post_opt: Remount /proc in jail");
    res = remount_proc();
    if (res != 0) {
        return 300 + res;
    }

    return ESPANK_SUCCESS;
}
