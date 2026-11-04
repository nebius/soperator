package v1

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

// nolint:unused
// log is for logging in this package.
var secretlog = logf.Log.WithName("secret-resource")

// SetupSecretWebhookWithManager registers the webhook for Secret in the manager.
func SetupSecretWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &corev1.Secret{}).
		WithValidator(&SecretCustomValidator{Client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/validate--v1-secret,mutating=false,failurePolicy=fail,sideEffects=None,groups="",resources=secrets,verbs=create;update;delete,versions=v1,name=vsecret-v1.kb.io,admissionReviewVersions=v1

// SecretCustomValidator struct is responsible for validating the Secret resource
// when it is created, updated, or deleted.
type SecretCustomValidator struct {
	Client client.Client
}

var _ admission.Validator[*corev1.Secret] = &SecretCustomValidator{}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type Secret.
func (v *SecretCustomValidator) ValidateCreate(ctx context.Context, secret *corev1.Secret) (admission.Warnings, error) {
	secretlog.Info("Validation for Secret upon creation", "name", secret.GetName())
	_, annotationExists := secret.Annotations[consts.AnnotationClusterName]
	if !annotationExists {
		return nil, fmt.Errorf("secret must have an annotation '%s'", consts.AnnotationClusterName)
	}
	return nil, nil
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type Secret.
func (v *SecretCustomValidator) ValidateUpdate(ctx context.Context, oldSecret, newSecret *corev1.Secret) (admission.Warnings, error) {
	secretlog.Info("Validation for Secret upon update", "name", newSecret.GetName())

	// Always deny update operations
	return nil, fmt.Errorf("update operations are not allowed for Secret resources")
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type Secret.
func (v *SecretCustomValidator) ValidateDelete(ctx context.Context, secret *corev1.Secret) (admission.Warnings, error) {
	secretlog.Info("Validation for Secret upon deletion", "name", secret.GetName())
	clusterAnnotation, annotationExists := secret.Annotations[consts.AnnotationClusterName]
	if annotationExists {
		// Check if SlurmCluster exists with the name from the annotation
		annotatedSlurmCluster := &slurmv1.SlurmCluster{}
		err := v.Client.Get(ctx, client.ObjectKey{
			Namespace: secret.Namespace,
			Name:      clusterAnnotation,
		}, annotatedSlurmCluster)

		if err == nil {
			return nil, fmt.Errorf("cannot delete Secret because referenced SlurmCluster '%s' exists", clusterAnnotation)
		}
	}

	// Allow delete operations if slurmcluster resource does not exist
	return nil, nil
}
