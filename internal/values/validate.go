package values

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/utils"
)

// Validate check whether the values are valid.
// Returns true if valid. Otherwise, false.
func (c *SlurmCluster) Validate(ctx context.Context) error {
	logger := log.FromContext(ctx)
	// PartitionConfiguration
	{
		if c.PartitionConfiguration.ConfigType == "custom" {
			for _, l := range c.PartitionConfiguration.RawConfig {
				line := strings.TrimSpace(l)
				if !strings.HasPrefix(line, "PartitionName") {
					err := fmt.Errorf("partition configuration should start with PartitionName")
					logger.Error(err, "partition configuration is invalid")
					return err
				}
			}
		}
	}

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
			volumeSourceNamesRaw = append(volumeSourceNamesRaw, subMount.VolumeSourceName)
		}
		// login jail sub-mounts
		for _, subMount := range c.NodeLogin.JailSubMounts {
			volumeSourceNamesRaw = append(volumeSourceNamesRaw, subMount.VolumeSourceName)
		}
		// worker custom mounts
		for _, customMount := range c.NodeWorker.CustomVolumeMounts {
			volumeSourceNamesRaw = append(volumeSourceNamesRaw, customMount.VolumeSourceName)
		}
		// login custom mounts
		for _, customMount := range c.NodeLogin.CustomVolumeMounts {
			volumeSourceNamesRaw = append(volumeSourceNamesRaw, customMount.VolumeSourceName)
		}
		// controller custom mounts
		for _, customMount := range c.NodeController.CustomVolumeMounts {
			volumeSourceNamesRaw = append(volumeSourceNamesRaw, customMount.VolumeSourceName)
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
					"NCCLBenchmark requires jail specified in VolumeSources",
					"Slurm.VolumeSource.Name", consts.VolumeNameJail,
				)
				return fmt.Errorf(
					"NCCLBenchmark requires volume source with name %q specified in VolumeSources: %w",
					consts.VolumeNameJail,
					err,
				)
			}
		}
	}

	// Secrets
	{
		loginNodeCount := c.NodeLogin.Size
		if loginNodeCount > 0 && c.NodeLogin.SSHRootPublicKeys == nil {
			err := fmt.Errorf("SSHRootPublicKeys are invalid. login node size %d (used) is specified, but SSH public keys are not provided", loginNodeCount)
			logger.Error(
				err,
				"SSHRootPublicKeys are invalid. login nodes are used, but SSH public keys are not provided",
				"Slurm.LoginNode.Count", loginNodeCount,
			)
			return err
		}

		if c.Secrets.SshdKeysName == "" {
			logger.V(1).Info("SshdKeysName is empty. Using default name")
			c.Secrets.SshdKeysName = naming.BuildSecretSSHDKeysName(c.Name)
		}
	}

	if err := utils.ExecuteMultiStep(ctx,
		"Login nodes validation",
		utils.MultiStepExecutionStrategyCollectErrors,
		utils.MultiStepExecutionStep{
			Name: "SshdServiceType valid",
			Func: func(stepCtx context.Context) error {
				if slices.Contains(
					[]string{
						string(corev1.ServiceTypeLoadBalancer),
						string(corev1.ServiceTypeNodePort),
					},
					string(c.NodeLogin.Service.Type),
				) {
					return nil
				}

				err := fmt.Errorf("SshdServiceType is invalid. It must be one of %q or %q", string(corev1.ServiceTypeLoadBalancer), string(corev1.ServiceTypeNodePort))
				log.FromContext(stepCtx).Error(
					err,
					"SshdServiceType is invalid",
					"Slurm.LoginNode.SshdServiceType", string(c.NodeLogin.Service.Type),
				)
				return err
			},
		},
		utils.MultiStepExecutionStep{
			Name: "SshdServiceNodePort specified",
			Func: func(stepCtx context.Context) error {
				if c.NodeLogin.Service.Type != corev1.ServiceTypeNodePort {
					return nil
				}
				if c.NodeLogin.Service.NodePort != 0 {
					return nil
				}

				err := fmt.Errorf("SshdServiceNodePort is not specified, but SshdServiceType %q is used", string(corev1.ServiceTypeNodePort))
				log.FromContext(stepCtx).Error(
					err,
					"SshdServiceNodePort is not specified",
					"Slurm.LoginNode.SshdServiceType", string(c.NodeLogin.Service.Type),
				)
				return err
			},
		},
		utils.MultiStepExecutionStep{
			Name: "SshdServiceNodePort valid",
			Func: func(stepCtx context.Context) error {
				if c.NodeLogin.Service.Type != corev1.ServiceTypeNodePort {
					return nil
				}
				if c.NodeLogin.Service.NodePort >= 30000 && c.NodeLogin.Service.NodePort <= 32768 {
					return nil
				}

				err := fmt.Errorf("SshdServiceNodePort is invalid. It must be in range 30000-32768, got %d", c.NodeLogin.Service.NodePort)
				log.FromContext(stepCtx).Error(
					err,
					"SshdServiceNodePort is invalid. It must be in range 30000-32768",
					"Slurm.LoginNode.SshdServiceType", string(c.NodeLogin.Service.Type),
					"Slurm.LoginNode.SshdServiceNodePort", c.NodeLogin.Service.NodePort,
				)
				return err
			},
		},
	); err != nil {
		return err
	}

	return nil
}
