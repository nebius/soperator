package common

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMergePodTemplateSpecs(t *testing.T) {
	tests := []struct {
		name      string
		baseSpec  corev1.PodTemplateSpec
		refSpec   corev1.PodTemplateSpec
		expectErr bool
	}{
		{
			name: "Merge labels and annotations",
			baseSpec: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"app": "test"},
					Annotations: map[string]string{"annotation1": "value1"},
				},
			},
			refSpec: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"env": "dev"},
					Annotations: map[string]string{"annotation2": "value2"},
				},
			},
			expectErr: false,
		},
		{
			name: "Merge containers",
			baseSpec: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "base-container", Image: "base-image"}},
				},
			},
			refSpec: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "ref-container", Image: "ref-image"}},
				},
			},
			expectErr: false,
		},
		{
			name: "Identical specs",
			baseSpec: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"app": "test"},
					Annotations: map[string]string{"annotation1": "value1"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app-container", Image: "app-image"}},
				},
			},
			refSpec: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"app": "test"},
					Annotations: map[string]string{"annotation1": "value1"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app-container", Image: "app-image"}},
				},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mergedSpec, err := MergePodTemplateSpecs(tt.baseSpec, &tt.refSpec)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, mergedSpec)

				mergedJSON, _ := json.Marshal(mergedSpec)
				baseJSON, _ := json.Marshal(tt.baseSpec)
				refJSON, _ := json.Marshal(tt.refSpec)

				if tt.name == "Identical specs" {
					assert.Equal(t, string(baseJSON), string(mergedJSON), "Merged spec should be identical to base when both specs are the same")
				} else {
					assert.NotEqual(t, string(baseJSON), string(mergedJSON), "Merged spec should differ from base")
					assert.NotEqual(t, string(refJSON), string(mergedJSON), "Merged spec should differ from reference")
				}
			}
		})
	}
}
