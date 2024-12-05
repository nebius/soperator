package accounting

import (
	"fmt"

	"github.com/sethvargo/go-password/password"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

func RenderSecretMariaDb(
	namespace,
	secretName,
	clusterName string,
) (*corev1.Secret, error) {
	generator, err := password.NewGenerator(&password.GeneratorInput{
		Symbols: "@$^&*()_+-={}|[]<>/",
	})
	if err != nil {
		return nil, fmt.Errorf("error creating password generator: %v", err)
	}
	password, err := generator.Generate(16, 4, 2, false, false)
	if err != nil {
		return nil, fmt.Errorf("error generating password Secret: %v", err)
	}

	annotations := map[string]string{
		consts.AnnotationClusterName: clusterName,
	}
	labels := map[string]string{
		consts.LabelNameKey:     consts.LabelNameValue,
		consts.LabelValidateKey: consts.LabelValidateValue,
	}

	data := map[string][]byte{
		"password": []byte(password),
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        secretName,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Data: data,
	}, nil
}
