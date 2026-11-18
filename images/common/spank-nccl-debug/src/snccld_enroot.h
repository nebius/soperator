/// Enroot definitions.

#ifndef SNCCLD_ENROOT_H
#define SNCCLD_ENROOT_H

#include <slurm/spank.h>

#define SNCCLD_ENROOT_STATE_DIR_ENV "_SNCCLD_ENROOT_STATE_DIR"
#define SNCCLD_ENROOT_LOG_DIR_ENV "_SNCCLD_ENROOT_LOG_DIR"

/// Clear inherited mount requests before evaluating this step's configuration.
spank_err_t snccld_enroot_clear_mounts(spank_t spank);

/// Publish resolved directories to the Enroot hook; log_dir may be empty.
spank_err_t snccld_enroot_set_mounts(
    spank_t spank, const char *state_dir, const char *log_dir
);

#endif // SNCCLD_ENROOT_H
