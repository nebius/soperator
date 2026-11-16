package sharedsteps

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const kruiseStatefulSetResource = "statefulsets.apps.kruise.io"

func findMetaCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func k8sNodeReady(node corev1.Node) bool {
	return kubeobjects.NodeReady(node)
}

type helmReleaseList struct {
	Items []helmRelease `json:"items"`
}

type helmRelease struct {
	Metadata metav1.ObjectMeta    `json:"metadata"`
	Status   helmReleaseStatusRef `json:"status"`
}

type helmReleaseStatusRef struct {
	Conditions []metav1.Condition `json:"conditions"`
}
