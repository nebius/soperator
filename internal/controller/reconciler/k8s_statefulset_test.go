package reconciler

import (
	"context"
	"testing"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

func TestAnnotationsMatch(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add appsv1 scheme: %v", err)
	}

	cluster := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	tests := []struct {
		name        string
		existing    *appsv1.StatefulSet
		desired     *appsv1.StatefulSet
		expectMatch bool
	}{
		{
			name: "Annotations match",
			existing: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing",
					Namespace: "default",
				},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"annotation1": "value1",
								"annotation2": "value2",
							},
						},
					},
				},
			},
			desired: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "desired",
					Namespace: "default",
				},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"annotation1": "value1",
								"annotation2": "value2",
								"versions": `soperator.soperator-munge: "2"
soperator.soperator-slurm-configs: "2"
soperator.soperator-slurmdbd-configs: "2"`,
							},
						},
					},
				},
			},
			expectMatch: true,
		},
		{
			name: "Annotations do not match",
			existing: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing",
					Namespace: "default",
				},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"annotation1": "value1",
								"annotation2": "value2",
								"versions": `soperator.soperator-munge: "1"
soperator.soperator-slurm-configs: "1"
soperator.soperator-slurmdbd-configs: "1"`,
							},
						},
					},
				},
			},
			desired: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "desired",
					Namespace: "default",
				},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"annotation1": "value1",
								"annotation2": "different_value",
								"versions": `soperator.soperator-munge: "3"
soperator.soperator-slurm-configs: "3"
soperator.soperator-slurmdbd-configs: "3"`,
							},
						},
					},
				},
			},
			expectMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &StatefulSetReconciler{
				Reconciler: &Reconciler{
					Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.existing).Build(),
				},
			}
			// Emulate the creation of the StatefulSet
			patch, err := r.patch(tt.desired, tt.desired)
			if err != nil {
				t.Fatalf("patch function returned an error: %v", err)
			}
			if err := r.Reconciler.EnsureUpdated(context.TODO(), cluster, tt.existing, tt.desired, patch); err != nil {
				t.Fatalf("failed to ensure StatefulSet is updated: %v", err)
			}
			// Emulate the update of the StatefulSet
			patch, err = r.patch(tt.existing, tt.desired)
			if err != nil {
				t.Fatalf("patch function returned an error: %v", err)
			}
			if err := r.Reconciler.EnsureUpdated(context.TODO(), cluster, tt.existing, tt.desired, patch); err != nil {
				t.Fatalf("failed to ensure StatefulSet is updated: %v", err)
			}
			match := equality.Semantic.DeepEqual(tt.existing.Spec.Template.ObjectMeta.Annotations["versions"], tt.desired.Spec.Template.ObjectMeta.Annotations["versions"])
			if match != tt.expectMatch {
				t.Errorf("Annotations match expectation failed. Expected: %v, Got: %v", tt.expectMatch, match)
			}
		})
	}
}

func TestAdvancedStatefulSetPatchCopiesPVCDeletionPolicy(t *testing.T) {
	existing := &kruisev1b1.StatefulSet{
		Spec: kruisev1b1.StatefulSetSpec{
			PersistentVolumeClaimRetentionPolicy: &kruisev1b1.StatefulSetPersistentVolumeClaimRetentionPolicy{
				WhenDeleted: kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType,
				WhenScaled:  kruisev1b1.RetainPersistentVolumeClaimRetentionPolicyType,
			},
		},
	}
	desired := &kruisev1b1.StatefulSet{
		Spec: kruisev1b1.StatefulSetSpec{
			PersistentVolumeClaimRetentionPolicy: &kruisev1b1.StatefulSetPersistentVolumeClaimRetentionPolicy{
				WhenDeleted: kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType,
				WhenScaled:  kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType,
			},
		},
	}

	r := &AdvancedStatefulSetReconciler{}
	if _, err := r.patch(existing, desired); err != nil {
		t.Fatalf("patch returned error: %v", err)
	}

	if existing.Spec.PersistentVolumeClaimRetentionPolicy == nil {
		t.Fatal("expected PersistentVolumeClaimRetentionPolicy to be copied")
	}

	if existing.Spec.PersistentVolumeClaimRetentionPolicy.WhenScaled != kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType {
		t.Fatalf(
			"expected WhenScaled=%q, got %q",
			kruisev1b1.DeletePersistentVolumeClaimRetentionPolicyType,
			existing.Spec.PersistentVolumeClaimRetentionPolicy.WhenScaled,
		)
	}
}

func TestStatefulSetPatchCopiesPVCDeletionPolicy(t *testing.T) {
	existing := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
				WhenDeleted: appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
				WhenScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
			},
		},
	}
	desired := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
				WhenDeleted: appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
				WhenScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
			},
		},
	}

	r := &StatefulSetReconciler{}
	if _, err := r.patch(existing, desired); err != nil {
		t.Fatalf("patch returned error: %v", err)
	}

	if existing.Spec.PersistentVolumeClaimRetentionPolicy == nil {
		t.Fatal("expected PersistentVolumeClaimRetentionPolicy to be copied")
	}

	if existing.Spec.PersistentVolumeClaimRetentionPolicy.WhenDeleted != appsv1.RetainPersistentVolumeClaimRetentionPolicyType {
		t.Fatalf(
			"expected WhenDeleted=%q, got %q",
			appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
			existing.Spec.PersistentVolumeClaimRetentionPolicy.WhenDeleted,
		)
	}
	if existing.Spec.PersistentVolumeClaimRetentionPolicy.WhenScaled != appsv1.RetainPersistentVolumeClaimRetentionPolicyType {
		t.Fatalf(
			"expected WhenScaled=%q, got %q",
			appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
			existing.Spec.PersistentVolumeClaimRetentionPolicy.WhenScaled,
		)
	}
}
