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

package v1alpha1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

// nodesetLog is for logging in this package.
var nodesetLog = logf.Log.WithName("nodeset-resource")

// SetupNodeSetWebhookWithManager registers the webhook for NodeSet in the manager.
func SetupNodeSetWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&slurmv1alpha1.NodeSet{}).
		WithValidator(&NodeSetCustomValidator{}).
		WithDefaulter(&NodeSetCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-slurm-nebius-ai-v1alpha1-nodeset,mutating=true,failurePolicy=fail,sideEffects=None,groups=slurm.nebius.ai,resources=nodesets,verbs=create;update,versions=v1alpha1,name=mnodeset-v1alpha1.kb.io,admissionReviewVersions=v1

// NodeSetCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind NodeSet when those are created or updated.
type NodeSetCustomDefaulter struct{}

var _ webhook.CustomDefaulter = &NodeSetCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind NodeSet.
func (d *NodeSetCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	nodeset, ok := obj.(*slurmv1alpha1.NodeSet)

	if !ok {
		return fmt.Errorf("expected an NodeSet object but got %T", obj)
	}
	nodesetLog.Info("Defaulting for NodeSet", "name", nodeset.GetName())

	return nil
}

// +kubebuilder:webhook:path=/validate-slurm-nebius-ai-v1alpha1-nodeset,mutating=false,failurePolicy=fail,sideEffects=None,groups=slurm.nebius.ai,resources=nodesets,verbs=create;update,versions=v1alpha1,name=vnodeset-v1alpha1.kb.io,admissionReviewVersions=v1

// NodeSetCustomValidator struct is responsible for validating the NodeSet resource
// when it is created, updated, or deleted.
type NodeSetCustomValidator struct{}

var _ webhook.CustomValidator = &NodeSetCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type NodeSet.
func (v *NodeSetCustomValidator) ValidateCreate(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type NodeSet.
func (v *NodeSetCustomValidator) ValidateUpdate(_ context.Context, _, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type NodeSet.
func (v *NodeSetCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
