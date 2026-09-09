package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/values"
)

func TestGenerateDefaultSupervisordConfig_StartsSSHDDirectly(t *testing.T) {
	rendered := generateDefaultSupervisordConfig().Render()

	assert.Contains(t, rendered, "command=/usr/sbin/sshd -D -e -f /mnt/ssh-configs/sshd_config")
	assert.NotContains(t, rendered, "sshd_pam_jail_entrypoint.sh")
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

func TestGenerateSshdConfig_KeepsChrootFallbackForOldImages(t *testing.T) {
	login := &values.SlurmLogin{
		ContainerSshd: values.Container{
			NodeContainer: slurmv1.NodeContainer{Port: 22},
		},
	}

	rendered := generateSshdConfig(login).Render()
	assert.Contains(t, rendered, "UsePAM yes")
	assert.Contains(t, rendered, "# Upgrade fallback for pre-PAM images")
	assert.Contains(t, rendered, "ChrootDirectory /mnt/jail")
}
