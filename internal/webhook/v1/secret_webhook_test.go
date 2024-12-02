package v1_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/uuid"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	. "nebius.ai/slurm-operator/internal/webhook/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestValidateCreate(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	validator := &SecretCustomValidator{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
	}

	tests := []struct {
		name        string
		annotations map[string]string
		expectError bool
	}{
		{
			name:        "TestValidateCreate_False",
			annotations: nil,
			expectError: true,
		},
		{
			name: "TestValidateCreate_True",
			annotations: map[string]string{
				consts.AnnotationClusterName: "test",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-secret",
					Namespace:   "default",
					Annotations: tt.annotations,
				},
			}

			_, err := validator.ValidateCreate(context.Background(), secret)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateUpdate(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	validator := &SecretCustomValidator{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
	}

	oldSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
	}

	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
	}

	_, err := validator.ValidateUpdate(context.Background(), oldSecret, newSecret)
	assert.Error(t, err, "ValidateUpdate must deny update")
}

func TestValidateDelete(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = slurmv1.AddToScheme(scheme)

	clientBuilder := fake.NewClientBuilder().WithScheme(scheme)
	fakeClient := clientBuilder.Build()

	validator := &SecretCustomValidator{
		Client: fakeClient,
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
			Annotations: map[string]string{
				consts.AnnotationClusterName: "test",
			},
		},
	}

	slurmCluster := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			UID:       uuid.NewUUID(),
		},
	}
	_ = fakeClient.Create(context.Background(), slurmCluster)

	_, err := validator.ValidateDelete(context.Background(), secret)
	assert.Error(t, err, "ValidateDelete must deny deletion if SlurmCluster exists")

	_ = fakeClient.Delete(context.Background(), slurmCluster)

	_, err = validator.ValidateDelete(context.Background(), secret)
	assert.NoError(t, err, "ValidateDelete must allow deletion if SlurmCluster does not exist")
}
