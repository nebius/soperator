package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoginWorkloadSelector(t *testing.T) {
	workload := loginWorkload{clusterName: "test-cluster"}

	assert.Equal(t,
		"app.kubernetes.io/instance=test-cluster,app.kubernetes.io/component=login",
		workload.selector(),
	)
}
