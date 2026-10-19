package common_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"nebius.ai/slurm-operator/internal/render/common"
)

func TestMergeExtraLabels(t *testing.T) {
	labels := map[string]string{"owned": "operator"}
	common.MergeExtraLabels(labels, map[string]string{"owned": "user", "extra": "user"})

	assert.Equal(t, map[string]string{"owned": "operator", "extra": "user"}, labels)
}

func TestMergeExtraAnnotations(t *testing.T) {
	annotations := map[string]string{"owned": "operator"}
	common.MergeExtraAnnotations(annotations, map[string]string{"owned": "user", "extra": "user"})

	assert.Equal(t, map[string]string{"owned": "operator", "extra": "user"}, annotations)
}
