package reconciler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
)

func TestGenerateDependencyVersionMap(t *testing.T) {
	tests := []struct {
		name       string
		dependency metav1.Object
		expected   map[string]string
	}{
		{
			name: "uses resource version by default",
			dependency: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "default-config",
					Namespace:       "soperator",
					ResourceVersion: "12345",
				},
			},
			expected: map[string]string{
				"soperator.default-config": "12345",
			},
		},
		{
			name: "uses name when dependency version annotation requests it",
			dependency: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "custom-supervisord-config-4.0.2",
					Namespace:       "soperator",
					ResourceVersion: "12345",
					Annotations: map[string]string{
						consts.AnnotationDependencyVersion: consts.AnnotationDependencyVersionName,
					},
				},
			},
			expected: map[string]string{
				"soperator.custom-supervisord-config-4.0.2": "custom-supervisord-config-4.0.2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, generateDependencyVersionMap(tt.dependency))
		})
	}
}
