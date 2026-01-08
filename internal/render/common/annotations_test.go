package common_test

import (
	"reflect"
	"testing"

	"nebius.ai/slurm-operator/internal/consts"
	. "nebius.ai/slurm-operator/internal/render/common"
)

func TestRenderDefaultContainerAnnotation(t *testing.T) {
	containerName := "test-container"
	expected := map[string]string{
		consts.AnnotationDefaultContainerName: containerName,
	}
	result := RenderDefaultContainerAnnotation(containerName)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}
