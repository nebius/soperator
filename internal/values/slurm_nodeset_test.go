package values

import (
	"testing"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

func TestDefaultPersistentVolumeClaimRetentionPolicy(t *testing.T) {
	t.Run("defaults to delete for both fields when unset", func(t *testing.T) {
		got := defaultPersistentVolumeClaimRetentionPolicy(nil)
		if got == nil {
			t.Fatal("expected default policy, got nil")
		}
		if got.WhenDeleted != kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType {
			t.Fatalf("expected whenDeleted=%q, got %q", kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType, got.WhenDeleted)
		}
		if got.WhenScaled != kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType {
			t.Fatalf("expected whenScaled=%q, got %q", kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType, got.WhenScaled)
		}
	})

	t.Run("keeps explicit delete values", func(t *testing.T) {
		got := defaultPersistentVolumeClaimRetentionPolicy(&slurmv1alpha1.PersistentVolumeClaimRetentionPolicy{
			WhenDeleted: slurmv1alpha1.PersistentVolumeClaimRetentionPolicyTypeDelete,
			WhenScaled:  slurmv1alpha1.PersistentVolumeClaimRetentionPolicyTypeDelete,
		})
		if got.WhenDeleted != kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType {
			t.Fatalf("expected whenDeleted=%q, got %q", kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType, got.WhenDeleted)
		}
		if got.WhenScaled != kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType {
			t.Fatalf("expected whenScaled=%q, got %q", kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType, got.WhenScaled)
		}
	})

	t.Run("defaults missing fields independently", func(t *testing.T) {
		got := defaultPersistentVolumeClaimRetentionPolicy(&slurmv1alpha1.PersistentVolumeClaimRetentionPolicy{
			WhenScaled: slurmv1alpha1.PersistentVolumeClaimRetentionPolicyTypeDelete,
		})
		if got.WhenDeleted != kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType {
			t.Fatalf("expected whenDeleted=%q, got %q", kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType, got.WhenDeleted)
		}
		if got.WhenScaled != kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType {
			t.Fatalf("expected whenScaled=%q, got %q", kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType, got.WhenScaled)
		}
	})

	t.Run("defaults missing fields independently to delete", func(t *testing.T) {
		got := defaultPersistentVolumeClaimRetentionPolicy(&slurmv1alpha1.PersistentVolumeClaimRetentionPolicy{
			WhenScaled: slurmv1alpha1.PersistentVolumeClaimRetentionPolicyTypeRetain,
		})
		if got.WhenDeleted != kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType {
			t.Fatalf("expected whenDeleted=%q, got %q", kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType, got.WhenDeleted)
		}
		if got.WhenScaled != kruisev1b1.RetainPersistentVolumeClaimRetentionPolicyType {
			t.Fatalf("expected whenScaled=%q, got %q", kruisev1b1.RetainPersistentVolumeClaimRetentionPolicyType, got.WhenScaled)
		}
	})
}
