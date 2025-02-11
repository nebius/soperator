package rest

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/jwt"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
)

func RenderSecret(namespace, clusterName string) (corev1.Secret, error) {
	secretName := naming.BuildSecretSlurmRESTSecretName(clusterName)
	labels := common.RenderLabels(consts.ComponentTypeREST, clusterName)
	key, err := jwt.GenerateSigningKey()
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
