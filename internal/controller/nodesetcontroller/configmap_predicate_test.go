package nodesetcontroller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestConfigMapDataChangedPredicate(t *testing.T) {
	predicate := configMapDataChangedPredicate()

	oldConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "custom-supervisord-config",
			Namespace: "soperator",
			Labels: map[string]string{
				"helm.sh/chart": "helm-soperator-custom-configmaps-4.0.1",
			},
		},
		Data: map[string]string{
			"supervisord.conf": "same",
		},
	}

	newMetadataOnly := oldConfigMap.DeepCopy()
	newMetadataOnly.Labels["helm.sh/chart"] = "helm-soperator-custom-configmaps-4.0.2"

	newData := oldConfigMap.DeepCopy()
	newData.Data["supervisord.conf"] = "changed"

	assert.False(t, predicate.Update(event.UpdateEvent{
		ObjectOld: oldConfigMap,
		ObjectNew: newMetadataOnly,
	}))
	assert.True(t, predicate.Update(event.UpdateEvent{
		ObjectOld: oldConfigMap,
		ObjectNew: newData,
	}))
}
