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
