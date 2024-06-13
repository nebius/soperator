#define _GNU_SOURCE
#include <slurm/spank.h>
#include <slurm/slurm_errno.h>
#include <stdlib.h>
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

int slurm_spank_task_init_privileged(spank_t spank, int argc, char **argv) {
    slurm_debug("chroot: slurm_spank_task_init_privileged: Enter jail environment");

    if (argc != 1) {
        fprintf(stderr, "expected 1 plugin argument: <path_to_jail>, but got %d arguments\n", argc);
        return 100;
    }
    char *jail_path = argv[0];

    int res = -1;

    slurm_debug("chroot: slurm_spank_task_init_privileged: Change the process root into %s", jail_path);
    res = change_root(jail_path);
    if (res != 0) {
        return 200 + res;
    }

    slurm_debug("chroot: slurm_spank_task_init_privileged: Remount /proc in jail");
    res = remount_proc();
    if (res != 0) {
        return 300 + res;
    }

    return ESPANK_SUCCESS;
}

int slurm_spank_task_exit(spank_t spank, int argc, char **argv) {
    slurm_debug("chroot: slurm_spank_task_exit: Enter jail environment");

    if (argc != 1) {
        fprintf(stderr, "expected 1 plugin argument: <path_to_jail>, but got %d arguments\n", argc);
        return 100;
    }
    char *jail_path = argv[0];

    slurm_debug("chroot: slurm_spank_task_exit: Change the process root into %s", jail_path);
    int res = change_root(jail_path);
    if (res != 0) {
        return 200 + res;
    }

    return ESPANK_SUCCESS;
}
