#ifndef SNCCLD_H
#define SNCCLD_H

#include <slurm/spank.h>

#define SNCCLD_PLUGIN_NAME nccl_debug

#define XSPANK_PLUGIN(__name, __ver) SPANK_PLUGIN(__name, __ver)

#define SNCCLD_NCCL_ENV_DEBUG         "NCCL_DEBUG"
#define SNCCLD_NCCL_ENV_DEBUG_FILE    "NCCL_DEBUG_FILE"
#define SNCCLD_NCCL_LOG_LEVEL_VERSION "VERSION"
#define SNCCLD_NCCL_LOG_LEVEL_WARN    "WARN"
#define SNCCLD_NCCL_LOG_LEVEL_INFO    "INFO"
#define SNCCLD_NCCL_LOG_LEVEL_TRACE   "TRACE"

#define SNCCLD_ENROOT_MOUNT_DIR           "/etc/enroot/mounts.d"
#define SNCCLD_ENROOT_MOUNT_TEMPLATE      "%s %s none %sbind,rw,nosuid\n"
#define SNCCLD_ENROOT_MOUNT_TEMPLATE_DIR  "x-create=dir,"
#define SNCCLD_ENROOT_MOUNT_TEMPLATE_FILE "x-create=file,"

#define SNCCLD_SYSTEM_DIR   "/tmp/nccl_debug"
#define SNCCLD_DEFAULT_MODE 0666

#define SNCCLD_LOG_PREFIX "SPANK | NCCL DEBUG"

#define SNCCLD_TEMPLATE_FILE_NAME "%s/%u-%u.%s"

#endif // SNCCLD_H
