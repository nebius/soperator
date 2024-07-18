package common

import (
	"crypto/rand"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

// region munge key

func generateRandBytes(size int) ([]byte, error) {
	randBytes := make([]byte, size)
	_, err := rand.Read(randBytes)
	if err != nil {
		return nil, err
	}
	return randBytes, nil
}

// RenderMungeKeySecret renders new [corev1.Secret] containing munge key
func RenderMungeKeySecret(clusterName string, namespace string) (corev1.Secret, error) {
	mungeKey, err := generateRandBytes(1024)
	if err != nil {
		return corev1.Secret{}, fmt.Errorf("error generating munge key: %w", err)
	}
	return corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildSecretMungeKeyName(clusterName),
			Namespace: namespace,
			Labels:    RenderLabels(consts.ComponentTypeCommon, clusterName),
		},
		Data: map[string][]byte{consts.SecretMungeKeyFileName: mungeKey},
	}, nil
}

// endregion munge key
