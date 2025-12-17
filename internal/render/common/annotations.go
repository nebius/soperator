package common

import (
	"nebius.ai/slurm-operator/internal/consts"
)

func RenderDefaultContainerAnnotation(defaultContainerName string) map[string]string {
	annotations := map[string]string{
		consts.AnnotationDefaultContainerName: defaultContainerName,
	}
	return annotations
}
