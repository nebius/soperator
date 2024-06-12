package values

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/utils"
)

// Validate check whether the values are valid.
// Returns true if valid. Otherwise, false.
func (c *SlurmCluster) Validate(ctx context.Context) error {
	logger := log.FromContext(ctx)

	// Node filters
	{
		// Node filters have unique names
		if nodeFiltersHaveUniqueNames := utils.ValidateUniqueEntries(
			c.NodeFilters,
			func(f slurmv1.K8sNodeFilter) string { return f.Name },
		); !nodeFiltersHaveUniqueNames {
			err := fmt.Errorf("k8s node filters are invalid. names must be unique")
			logger.Error(err, "K8sNodeFilters are invalid. Names must be unique")
			return err
		}

		// Node controller refers to existing node filter
		if _, err := utils.GetBy(
			c.NodeFilters,
			c.NodeController.K8sNodeFilterName,
			func(f slurmv1.K8sNodeFilter) string { return f.Name },
		); err != nil {
			logger.Error(
				err,
				"Specified k8s node filter name not found in K8sNodeFilters",
				"Slurm.Node", consts.ComponentTypeController,
				"Slurm.K8sNodeFilterName", c.NodeController.K8sNodeFilterName,
			)
			return fmt.Errorf(
				"specified k8s node filter name %q not found in K8sNodeFilters: %w",
				c.NodeController.K8sNodeFilterName,
				err,
			)
		}
	}

	// Volume sources
	{
		// Volume sources have unique names
		if volumeSourcesHaveUniqueNames := utils.ValidateUniqueEntries(
			c.VolumeSources,
			func(s slurmv1.VolumeSource) string { return s.Name },
		); !volumeSourcesHaveUniqueNames {
			err := fmt.Errorf("volume sources are invalid. names must be unique")
			logger.Error(err, "VolumeSources are invalid. Names must be unique")
		}

		// Volume sources have only one source
		for _, volumeSource := range c.VolumeSources {
			if onlyOneSourceSpecified := utils.ValidateOneOf(volumeSource.VolumeSource); onlyOneSourceSpecified {
				continue
			}

			err := fmt.Errorf(
				"volume source %q is invalid. only one of sources must be specified",
				volumeSource.Name,
			)
			logger.Error(
				err,
				"Volume source is invalid. Only one of sources must be specified",
				"Slurm.VolumeSource.Name", volumeSource.Name,
			)
			return err
		}

		var volumeSourceNames []string

		// Node volume source names are not empty
		volumeSourceNamesRaw := []*string{
			// controller
			c.NodeController.VolumeSpool.VolumeSourceName,
			c.NodeController.VolumeJail.VolumeSourceName,
			// worker
			c.NodeWorker.VolumeSpool.VolumeSourceName,
			c.NodeWorker.VolumeJail.VolumeSourceName,
		}
		// worker jail sub-mounts
		for _, subMount := range c.NodeWorker.JailSubMounts {
			volumeSourceNamesRaw = append(volumeSourceNamesRaw, &subMount.VolumeSourceName)
		}
		for _, volumeSourceName := range volumeSourceNamesRaw {
			if volumeSourceName == nil {
				continue
			}
			if *volumeSourceName == "" {
				err := fmt.Errorf("volume source name is invalid: empty")
				logger.Error(err, "Volume source name is invalid: empty")
			}
			volumeSourceNames = append(volumeSourceNames, *volumeSourceName)
		}

		// Node volume source names refer to existing volume sources
		for _, volumeSourceName := range volumeSourceNames {
			_, err := utils.GetBy(
				c.VolumeSources,
				volumeSourceName,
				func(f slurmv1.VolumeSource) string { return f.Name },
			)
			if err == nil {
				continue
			}

			logger.Error(
				err,
				"Specified node volume source name not found in VolumeSources",
				"Slurm.VolumeSource.Name", volumeSourceName,
			)
			return fmt.Errorf(
				"specified node volume source name %q not found in VolumeSources: %w",
				volumeSourceName,
				err,
			)
		}

		// NCCLBenchmark volume refer to existing volume sources
		if c.NCCLBenchmark.Enabled {
			_, err := utils.GetBy(
				c.VolumeSources,
				consts.VolumeNameJail,
				func(f slurmv1.VolumeSource) string { return f.Name },
			)
			if err != nil {
				logger.Error(
					err,
					"Specified volume source name for NCCLBenchmark not found in VolumeSources",
					"Slurm.VolumeSource.Name", consts.VolumeNameJail,
				)
			}
			return fmt.Errorf(
				"specified node volume source name %q not found in VolumeSources: %w",
				consts.VolumeNameJail,
				err,
			)
		}
	}

	// Secrets
	// TODO login node
	{
		//loginNodeCount := c.NodeLogin.Size
		//if loginNodeCount == 0 && clusterCrd.Spec.Secrets.SSHRootPublicKeys != nil {
		//	err := fmt.Errorf("secrets are invalid. login node size %d (unused) is specified, but SSH public keys provided", loginNodeCount)
		//	logger.Error(
		//		err,
		//		"Secrets are invalid. login nodes are unused, but SSH public keys provided",
		//		"Slurm.LoginNode.Count", loginNodeCount,
		//		"Slurm.Secret.SSHRootPublicKeys.Name", clusterCrd.Spec.Secrets.SSHRootPublicKeys.Name,
		//	)
		//	return slurmv1.Secrets{}, err
		//}
		//if loginNodeCount > 0 && clusterCrd.Spec.Secrets.SSHRootPublicKeys == nil {
		//	err := fmt.Errorf("secrets are invalid. login node size %d (used) is specified, but SSH public keys are not provided", loginNodeCount)
		//	logger.Error(
		//		err,
		//		"Secrets are invalid. login nodes are used, but SSH public keys are not provided",
		//		"Slurm.LoginNode.Count", loginNodeCount,
		//	)
		//	return slurmv1.Secrets{}, err
		//}
	}

	return nil
}
