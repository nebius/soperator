package common

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

const (
	DefaultPodTerminationGracePeriodSeconds = int64(30)
)

func MergePodTemplateSpecs(
	baseSpec corev1.PodTemplateSpec,
	refSpec *corev1.PodTemplateSpec,
) (corev1.PodTemplateSpec, error) {
	var result corev1.PodTemplateSpec

	originalJSON, err := json.Marshal(baseSpec)
	if err != nil {
		return corev1.PodTemplateSpec{}, fmt.Errorf("error marshalling original PodTemplateSpec: %v", err)
	}

	patchJSON, err := json.Marshal(refSpec)
	if err != nil {
		return corev1.PodTemplateSpec{}, fmt.Errorf("error marshalling patch PodTemplateSpec: %v", err)
	}

	mergedJSON, err := strategicpatch.StrategicMergePatch(originalJSON, patchJSON, &corev1.PodTemplateSpec{})
	if err != nil {
		return corev1.PodTemplateSpec{}, fmt.Errorf("error performing strategic merge: %v", err)
	}

	// Ummarshal the merged JSON back into a struct
	err = json.Unmarshal(mergedJSON, &result)
	if err != nil {
		return corev1.PodTemplateSpec{}, fmt.Errorf("error unmarshalling merged PodTemplateSpec: %v", err)
	}

	return result, nil
}
