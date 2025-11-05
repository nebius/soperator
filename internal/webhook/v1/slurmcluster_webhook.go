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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/feature"
)

// nolint:unused
// slurmclusterlog is for logging in this package.
var slurmclusterlog = logf.Log.WithName("slurmcluster-resource")

// SetupSlurmClusterWebhookWithManager registers the webhook for SlurmCluster in the manager.
func SetupSlurmClusterWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&slurmv1.SlurmCluster{}).
		WithValidator(&SlurmClusterCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-slurm-nebius-ai-v1-slurmcluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=slurm.nebius.ai,resources=slurmclusters,verbs=create;update,versions=v1,name=vslurmcluster-v1.kb.io,admissionReviewVersions=v1

// SlurmClusterCustomValidator struct is responsible for validating the SlurmCluster resource
// when it is created, updated, or deleted.
type SlurmClusterCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &SlurmClusterCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type SlurmCluster.
func (v *SlurmClusterCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	slurmcluster, ok := obj.(*slurmv1.SlurmCluster)
	if !ok {
		return nil, fmt.Errorf("expected a SlurmCluster object but got %T", obj)
	}
	slurmclusterlog.Info("Validation for SlurmCluster upon creation", "name", slurmcluster.GetName())

	if err := validateSlurmClusterStructuredPartitionRequireNodeSets(slurmcluster.Spec.PartitionConfiguration.ConfigType); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type SlurmCluster.
func (v *SlurmClusterCustomValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	slurmcluster, ok := newObj.(*slurmv1.SlurmCluster)
	if !ok {
		return nil, fmt.Errorf("expected a SlurmCluster object for the newObj but got %T", newObj)
	}
	slurmclusterlog.Info("Validation for SlurmCluster upon update", "name", slurmcluster.GetName())

	if err := validateSlurmClusterStructuredPartitionRequireNodeSets(slurmcluster.Spec.PartitionConfiguration.ConfigType); err != nil {
		return nil, err
	}

	return nil, nil
}

func validateSlurmClusterStructuredPartitionRequireNodeSets(configType string) error {
	if !feature.Gate.Enabled(feature.NodeSetWorkers) && configType == slurmv1.PartitionConfigTypeStructured {
		return fmt.Errorf("structured partitions are not supported with disabled nodesets")
	}

	return nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type SlurmCluster.
func (v *SlurmClusterCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
