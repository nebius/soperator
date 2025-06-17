/// Enroot definitions.

#ifndef SNCCLD_ENROOT_H
#define SNCCLD_ENROOT_H

/// Absolute path to the directory of Enroot mount configurations.
#define SNCCLD_ENROOT_MOUNT_DIR "/etc/enroot/mounts.d"

/// Template of the Enroot mount in fstab format.
#define SNCCLD_ENROOT_MOUNT_TEMPLATE "%s %s none %sbind,rw,nosuid\n"

/// Fstab flag to mount the path as a directory.
#define SNCCLD_ENROOT_MOUNT_TEMPLATE_DIR "x-create=dir,"

/// Fstab flag to mount the path as a file.
#define SNCCLD_ENROOT_MOUNT_TEMPLATE_FILE "x-create=file,"

#endif // SNCCLD_ENROOT_H
