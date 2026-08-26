package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func TestGenerateDefaultSupervisordConfig_UsesPAMJailEntrypoint(t *testing.T) {
	rendered := generateDefaultSupervisordConfig().Render()

	assert.Contains(t, rendered, "[ -x /opt/bin/slurm/sshd_pam_jail_entrypoint.sh ]")
	assert.Contains(t, rendered, "exec /opt/bin/slurm/sshd_pam_jail_entrypoint.sh")
	assert.Contains(t, rendered, "else exec /usr/sbin/sshd -D -e -f /mnt/ssh-configs/sshd_config")
}

func TestGenerateSshdConfig_AuthorizedKeysCommandDependsOnSSSD(t *testing.T) {
	login := &values.SlurmLogin{
		ContainerSshd: values.Container{
			NodeContainer: slurmv1.NodeContainer{Port: 22},
		},
	}

	withoutSSSD := generateSshdConfig(login).Render()
	assert.NotContains(t, withoutSSSD, "AuthorizedKeysCommand /usr/bin/sss_ssh_authorizedkeys")

	login.ContainerSSSD = &values.Container{
		NodeContainer: slurmv1.NodeContainer{Image: "sssd-image"},
	}

	withSSSD := generateSshdConfig(login).Render()
	assert.Contains(t, withSSSD, "AuthorizedKeysCommand /usr/bin/sss_ssh_authorizedkeys")
	assert.Contains(t, withSSSD, "AuthorizedKeysCommandUser root")
}

func TestGenerateSshdConfig_KeepsChrootForOldWorkerImages(t *testing.T) {
	login := &values.SlurmLogin{
		ContainerSshd: values.Container{
			NodeContainer: slurmv1.NodeContainer{Port: 22},
		},
	}

	rendered := generateSshdConfig(login).Render()
	assert.Contains(t, rendered, "UsePAM yes")
	assert.Contains(t, rendered, "ChrootDirectory "+consts.VolumeMountPathJail)
}
