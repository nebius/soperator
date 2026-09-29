package worker

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

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

func TestRenderConfigMapPAMSlurmAdopt(t *testing.T) {
	cluster := &values.SlurmCluster{
		PAMSlurmAdopt: values.PAMSlurmAdopt{
			Enabled:      true,
			ExemptUsers:  []string{"service-user", `EXAMPLE\domain-user`},
			ExemptGroups: []string{"cluster-admins", "platform operators"},
		},
	}
	cluster.Name = "test-cluster"
	cluster.Namespace = "test-namespace"

	result := RenderConfigMapPAMSlurmAdopt(cluster)

	assert.Equal(t, "test-cluster-pam-slurm-adopt", result.Name)
	assert.Equal(t, "test-namespace", result.Namespace)
	pamConfig := result.Data[consts.ConfigMapKeyPAMSlurmAdopt]
	assert.Contains(t, pamConfig,
		"account sufficient pam_listfile.so item=user sense=allow onerr=fail file=/etc/soperator/pam-slurm-adopt/..data/soperator-pam-slurm-adopt-users")
	assert.Contains(t, pamConfig,
		"account sufficient pam_listfile.so item=group sense=allow onerr=fail file=/etc/soperator/pam-slurm-adopt/..data/soperator-pam-slurm-adopt-groups")
	assert.Contains(t, pamConfig, "action_no_jobs=deny")
	assert.Contains(t, pamConfig, "action_unknown=newest")
	assert.Contains(t, pamConfig, "action_adopt_failure=deny")
	assert.Contains(t, pamConfig, "action_generic_failure=deny")
	assert.Contains(t, pamConfig, "disable_x11=1")
	assert.Contains(t, pamConfig, "join_container=false")
	lines := strings.Split(pamConfig, "\n")
	assert.Contains(t, lines[len(lines)-1], "pam_slurm_adopt.so")

	assert.Equal(t, "service-user\nEXAMPLE\\domain-user", result.Data[consts.ConfigMapKeyPAMSlurmAdoptUsers])
	assert.Equal(t, "cluster-admins\nplatform operators", result.Data[consts.ConfigMapKeyPAMSlurmAdoptGroups])
}

func TestRenderConfigMapPAMSlurmAdoptWithoutExemptions(t *testing.T) {
	result := RenderConfigMapPAMSlurmAdopt(&values.SlurmCluster{
		PAMSlurmAdopt: values.PAMSlurmAdopt{Enabled: true},
	})

	assert.Contains(t, result.Data[consts.ConfigMapKeyPAMSlurmAdopt], "pam_listfile.so item=user")
	assert.Contains(t, result.Data[consts.ConfigMapKeyPAMSlurmAdopt], "pam_listfile.so item=group")
	assert.Contains(t, result.Data[consts.ConfigMapKeyPAMSlurmAdopt], "action_unknown=newest")
	assert.Empty(t, result.Data[consts.ConfigMapKeyPAMSlurmAdoptUsers])
	assert.Empty(t, result.Data[consts.ConfigMapKeyPAMSlurmAdoptGroups])
}

func TestRenderConfigMapPAMSlurmAdoptDisabled(t *testing.T) {
	result := RenderConfigMapPAMSlurmAdopt(&values.SlurmCluster{
		PAMSlurmAdopt: values.PAMSlurmAdopt{
			ExemptUsers:  []string{"service-user"},
			ExemptGroups: []string{"cluster-admins"},
		},
	})

	assert.Empty(t, result.Data[consts.ConfigMapKeyPAMSlurmAdopt])
	assert.Equal(t, "service-user", result.Data[consts.ConfigMapKeyPAMSlurmAdoptUsers])
	assert.Equal(t, "cluster-admins", result.Data[consts.ConfigMapKeyPAMSlurmAdoptGroups])
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
