package login

import (
	"testing"

	"github.com/stretchr/testify/assert"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/values"
)

func TestGenerateSshdConfig_AuthorizedKeysCommandDependsOnSSSD(t *testing.T) {
	cluster := &values.SlurmCluster{
		NodeLogin: values.SlurmLogin{
			ContainerSshd: values.Container{
				NodeContainer: slurmv1.NodeContainer{Port: 22},
			},
		},
	}

	withoutSSSD := generateSshdConfig(cluster).Render()
	assert.NotContains(t, withoutSSSD, "AuthorizedKeysCommand /usr/bin/sss_ssh_authorizedkeys")

	cluster.NodeLogin.ContainerSSSD = &values.Container{
		NodeContainer: slurmv1.NodeContainer{Image: "sssd-image"},
	}

	withSSSD := generateSshdConfig(cluster).Render()
	assert.Contains(t, withSSSD, "AuthorizedKeysCommand /usr/bin/sss_ssh_authorizedkeys")
	assert.Contains(t, withSSSD, "AuthorizedKeysCommandUser root")
}
