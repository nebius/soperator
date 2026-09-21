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
	"strings"
	"unicode"

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
	if err := validateLoginDocker(cluster); err != nil {
		return err
	}
	return validatePAMSlurmAdopt(cluster)
}

func validatePAMSlurmAdopt(cluster *slurmv1.SlurmCluster) error {
	config := cluster.Spec.PAMSlurmAdopt
	if config == nil {
		return nil
	}

	if err := validatePAMListfileIdentities("exemptUsers", config.ExemptUsers); err != nil {
		return err
	}
	if err := validatePAMListfileIdentities("exemptGroups", config.ExemptGroups); err != nil {
		return err
	}
	if !ptr.Deref(config.Enabled, false) || cluster.Spec.CustomSlurmConfig == nil {
		return nil
	}

	return validatePAMSlurmAdoptOverrides(*cluster.Spec.CustomSlurmConfig)
}

func validatePAMListfileIdentities(field string, identities []string) error {
	seen := make(map[string]struct{}, len(identities))
	for index, identity := range identities {
		if identity == "" || strings.TrimSpace(identity) != identity || strings.IndexFunc(identity, unicode.IsControl) >= 0 {
			return fmt.Errorf(
				"configure pamSlurmAdopt.%s[%d] as a non-empty identity without surrounding whitespace or control characters",
				field,
				index,
			)
		}
		if _, exists := seen[identity]; exists {
			return fmt.Errorf("remove duplicate identity %q from pamSlurmAdopt.%s", identity, field)
		}
		seen[identity] = struct{}{}
	}
	return nil
}

func validatePAMSlurmAdoptOverrides(customConfig string) error {
	requiredParameters := []struct {
		key   string
		value string
	}{
		{key: "prologflags", value: "contain"},
		{key: "launchparameters", value: "ulimit_pam_adopt"},
	}
	effectiveValues := make(map[string]string, len(requiredParameters))
	for line := range strings.Lines(customConfig) {
		line, _, _ = strings.Cut(line, "#")
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		for _, parameter := range requiredParameters {
			if key == parameter.key {
				effectiveValues[key] = strings.TrimSpace(value)
				break
			}
		}
	}

	for _, parameter := range requiredParameters {
		value, overridden := effectiveValues[parameter.key]
		if overridden && !containsSlurmConfigParameter(value, parameter.value) {
			return fmt.Errorf(
				"include %q in customSlurmConfig %s when pamSlurmAdopt is enabled",
				parameter.value,
				canonicalSlurmConfigKey(parameter.key),
			)
		}
	}
	return nil
}

func containsSlurmConfigParameter(value, expected string) bool {
	for parameter := range strings.SplitSeq(value, ",") {
		if strings.EqualFold(strings.TrimSpace(parameter), expected) {
			return true
		}
	}
	return false
}

func canonicalSlurmConfigKey(key string) string {
	if key == "prologflags" {
		return "PrologFlags"
	}
	return "LaunchParameters"
}

func validateLoginDocker(cluster *slurmv1.SlurmCluster) error {
	login := &cluster.Spec.SlurmNodes.Login
	for _, env := range login.Sshd.CustomEnv {
		if env.Name == consts.EnvDockerEnabled {
			return fmt.Errorf(
				"remove environment variable %q from login.sshd.customEnv because it is managed by Soperator",
				consts.EnvDockerEnabled,
			)
		}
	}
	if login.Docker == nil || !ptr.Deref(login.Docker.Enabled, false) {
		return nil
	}
	if login.UserIsolation == nil || !ptr.Deref(login.UserIsolation.Enabled, false) {
		return fmt.Errorf("configure login.userIsolation.enabled=true when login Docker is enabled")
	}

	for _, mount := range login.Volumes.JailSubMounts {
		if mount.MountPath != consts.ImageStorageMountPath {
			continue
		}
		if mount.ReadOnly {
			return fmt.Errorf("configure login Docker image storage at %s as writable", consts.ImageStorageMountPath)
		}
		return nil
	}

	return fmt.Errorf("configure a writable login jail sub-mount at %s when login Docker is enabled", consts.ImageStorageMountPath)
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
