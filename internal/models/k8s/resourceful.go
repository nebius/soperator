package k8smodels

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

type Resourceful struct {
	Affinity    *corev1.Affinity
	Tolerations []corev1.Toleration
	Resources   corev1.ResourceList
}

func BuildResourcefulFrom(podSpec *slurmv1.PodSpec) (Resourceful, error) {
	var err error

	res := Resourceful{
		Affinity:    BuildAffinityFrom(podSpec),
		Tolerations: BuildTolerationsFrom(podSpec),
	}

	res.Resources, err = BuildResourcesFrom(podSpec)
	if err != nil {
		return Resourceful{}, err
	}

	return res, nil
}

func BuildAffinityFrom(podSpec *slurmv1.PodSpec) *corev1.Affinity {
	if podSpec != nil && podSpec.Affinity != nil {
		return podSpec.Affinity.DeepCopy()
	}
	return nil
}

func BuildTolerationsFrom(podSpec *slurmv1.PodSpec) []corev1.Toleration {
	if podSpec != nil && podSpec.Tolerations != nil {
		return podSpec.Tolerations
	}
	return []corev1.Toleration{}
}

func BuildResourcesFrom(podSpec *slurmv1.PodSpec) (corev1.ResourceList, error) {
	res := corev1.ResourceList{}

	if podSpec == nil {
		return res, nil
	}

	for _, v := range []struct {
		key corev1.ResourceName
		val *string
	}{
		{key: corev1.ResourceCPU, val: podSpec.Cores},
		{key: corev1.ResourceMemory, val: podSpec.Memory},
	} {
		if v.val == nil {
			continue
		}
		q, err := resource.ParseQuantity(*v.val)
		if err != nil {
			return corev1.ResourceList{}, err
		}
		res[v.key] = q
	}

	return res, nil
}
