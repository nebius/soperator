package rest

import (
	"crypto/rand"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
)

func RenderSecret(namespace, clusterName string) (corev1.Secret, error) {
	secretName := naming.BuildSecretSlurmRESTSecretName(clusterName)
	labels := common.RenderLabels(consts.ComponentTypeREST, clusterName)
	key, err := generateSlurmRESTJWTKey()
	if err != nil {
		return corev1.Secret{}, err
	}

	data := map[string][]byte{consts.SecretRESTJWTKeyFileName: key}

	return corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels:    labels,
		},
		Data: data,
	}, nil
}

func generateSlurmRESTJWTKey() ([]byte, error) {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	if err != nil {
		return nil, err
	}
	return key, nil
}