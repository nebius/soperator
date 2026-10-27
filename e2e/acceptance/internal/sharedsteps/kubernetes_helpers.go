package sharedsteps

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const kruiseStatefulSetResource = "statefulsets.apps.kruise.io"

func findMetaCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
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
