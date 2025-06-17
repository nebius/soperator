#ifndef SNCCLD_H
#define SNCCLD_H

#include <slurm/spank.h>

#define SNCCLD_PLUGIN_NAME nccl_debug

#define XSPANK_PLUGIN(__name, __ver) SPANK_PLUGIN(__name, __ver)

#define SNCCLD_LOG_PREFIX "SPANK | NCCL DEBUG"

#endif // SNCCLD_H
