/*
Copyright 2025 Nebius B.V.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"context"
	"fmt"

	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

// nolint:unused
// slurmClusterLog is for logging in this package.
var slurmClusterLog = logf.Log.WithName("slurmcluster-resource")

// SetupSlurmClusterWebhookWithManager registers the webhook for SlurmCluster in the manager.
func SetupSlurmClusterWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &slurmv1.SlurmCluster{}).
		WithValidator(&SlurmClusterCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-slurm-nebius-ai-v1-slurmcluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=slurm.nebius.ai,resources=slurmclusters,verbs=create;update,versions=v1,name=vslurmcluster-v1.kb.io,admissionReviewVersions=v1

// SlurmClusterCustomValidator struct is responsible for validating the SlurmCluster resource
// when it is created, updated, or deleted.
type SlurmClusterCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ admission.Validator[*slurmv1.SlurmCluster] = &SlurmClusterCustomValidator{}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type SlurmCluster.
func (v *SlurmClusterCustomValidator) ValidateCreate(_ context.Context, slurmCluster *slurmv1.SlurmCluster) (admission.Warnings, error) {
	slurmClusterLog.Info("Validation for SlurmCluster upon creation", "name", slurmCluster.GetName())

	return nil, validateSlurmCluster(slurmCluster)
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type SlurmCluster.
func (v *SlurmClusterCustomValidator) ValidateUpdate(_ context.Context, _, newSlurmCluster *slurmv1.SlurmCluster) (admission.Warnings, error) {
	slurmClusterLog.Info("Validation for SlurmCluster upon update", "name", newSlurmCluster.GetName())

	return nil, validateSlurmCluster(newSlurmCluster)
}

func validateSlurmCluster(cluster *slurmv1.SlurmCluster) error {
	if err := validateLoginUserIsolation(cluster); err != nil {
		return err
	}
	return validateLoginDocker(cluster)
}

func validateLoginDocker(cluster *slurmv1.SlurmCluster) error {
	login := &cluster.Spec.SlurmNodes.Login
	if login.Docker == nil || !ptr.Deref(login.Docker.Enabled, false) {
		return nil
	}

	for _, mount := range login.Volumes.JailSubMounts {
		if mount.MountPath != consts.DockerImageStorageMountPath {
			continue
		}
		if mount.ReadOnly {
			return fmt.Errorf("login Docker image storage at %s must be writable", consts.DockerImageStorageMountPath)
		}
		return nil
	}

	return fmt.Errorf(
		"login Docker requires a dedicated jail sub-mount at %s",
		consts.DockerImageStorageMountPath,
	)
}

// validateLoginUserIsolation checks the effective per-user memory limits.
func validateLoginUserIsolation(cluster *slurmv1.SlurmCluster) error {
	isolation := cluster.Spec.SlurmNodes.Login.UserIsolation
	if isolation == nil || !ptr.Deref(isolation.Enabled, false) {
		return nil
	}

	if isolation.MemoryHigh != nil && isolation.MemoryHigh.Sign() <= 0 {
		return fmt.Errorf(
			"login.userIsolation.memoryHigh (%s) must be greater than zero",
			isolation.MemoryHigh.String(),
		)
	}
	if isolation.MemoryMax != nil && isolation.MemoryMax.Sign() <= 0 {
		return fmt.Errorf(
			"login.userIsolation.memoryMax (%s) must be greater than zero",
			isolation.MemoryMax.String(),
		)
	}

	containerMemory := cluster.Spec.SlurmNodes.Login.Sshd.Resources.Memory()
	memoryHigh, memoryMax := values.ResolveLoginUserIsolationMemoryLimits(isolation, containerMemory)
	if memoryHigh != nil && memoryMax != nil && memoryHigh.Cmp(*memoryMax) > 0 {
		return fmt.Errorf(
			"effective login.userIsolation.memoryHigh (%s) must not exceed memoryMax (%s)",
			memoryHigh.String(), memoryMax.String(),
		)
	}

	if containerMemory == nil || containerMemory.Sign() <= 0 {
		return nil
	}

	if isolation.MemoryHigh != nil && isolation.MemoryHigh.Cmp(*containerMemory) >= 0 {
		return fmt.Errorf(
			"login.userIsolation.memoryHigh (%s) must be lower than the sshd container memory limit (%s)",
			isolation.MemoryHigh.String(), containerMemory.String(),
		)
	}
	if isolation.MemoryMax != nil && isolation.MemoryMax.Cmp(*containerMemory) >= 0 {
		return fmt.Errorf(
			"login.userIsolation.memoryMax (%s) must be lower than the sshd container memory limit (%s)",
			isolation.MemoryMax.String(), containerMemory.String(),
		)
	}
	return nil
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type SlurmCluster.
func (v *SlurmClusterCustomValidator) ValidateDelete(_ context.Context, _ *slurmv1.SlurmCluster) (admission.Warnings, error) {
	return nil, nil
}
