package accounting_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	. "nebius.ai/slurm-operator/internal/render/accounting"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_RenderMariaDbGrant(t *testing.T) {
	namespace := "test-namespace"
	clusterName := "test-cluster"
	accounting := &values.SlurmAccounting{
		MariaDb: slurmv1.MariaDbOpeator{
			Enabled: true,
		},
	}

	grant, err := RenderMariaDbGrant(namespace, clusterName, accounting)

	assert.NoError(t, err)
	assert.NotNil(t, grant)
	assert.Equal(t, namespace, grant.Namespace)
	assert.Equal(t, clusterName+"-"+consts.MariaDbClusterSuffix, grant.Name)
	assert.Equal(t, "ALL PRIVILEGES", grant.Spec.Privileges[0])
}
