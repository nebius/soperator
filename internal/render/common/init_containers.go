package common

import (
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

// OrderInitContainers merges system init containers with custom init containers.
// Custom containers maintain their relative order from the array.
// If a custom container has RunsBefore, it is inserted before the leftmost
// referenced system container. RunsBefore can only reference system containers.
func OrderInitContainers(system []corev1.Container, custom []slurmv1.InitContainer) ([]corev1.Container, error) {
	return orderInitContainersGeneric(system, custom, func(c slurmv1.InitContainer) (corev1.Container, []string) {
		return c.Container, c.RunsBefore
	})
}

// OrderInitContainersAlpha is the same as OrderInitContainers but for v1alpha1.InitContainer.
func OrderInitContainersAlpha(system []corev1.Container, custom []slurmv1alpha1.InitContainer) ([]corev1.Container, error) {
	return orderInitContainersGeneric(system, custom, func(c slurmv1alpha1.InitContainer) (corev1.Container, []string) {
		return c.Container, c.RunsBefore
	})
}

func orderInitContainersGeneric[T any](
	system []corev1.Container,
	custom []T,
	extract func(T) (corev1.Container, []string),
) ([]corev1.Container, error) {
	result := slices.Clone(system)

	systemIndex := make(map[string]int)
	for i, c := range system {
		systemIndex[c.Name] = i
	}

	for _, c := range custom {
		container, runsBefore := extract(c)
		insertPos := len(result)

		for _, target := range runsBefore {
			pos, isSystem := systemIndex[target]
			if !isSystem {
				return nil, fmt.Errorf("runsBefore can only reference system containers, %q is not a system container (available: %v)", target, systemContainerNames(system))
			}
			if pos < insertPos {
				insertPos = pos
			}
		}

		result = slices.Insert(result, insertPos, container)

		for name, pos := range systemIndex {
			if pos >= insertPos {
				systemIndex[name] = pos + 1
			}
		}
	}

	return result, nil
}

func systemContainerNames(system []corev1.Container) []string {
	names := make([]string, len(system))
	for i, c := range system {
		names[i] = c.Name
	}
	return names
}
