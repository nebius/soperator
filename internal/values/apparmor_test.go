package values

import (
	"testing"

	"github.com/stretchr/testify/require"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

func TestAppArmorProfilesPreserved(t *testing.T) {
	for _, profile := range []string{"", "unconfined", "custom"} {
		t.Run(profile, func(t *testing.T) {
			login := &slurmv1.SlurmNodeLogin{
				Sshd:  slurmv1.NodeContainer{AppArmorProfile: profile},
				Munge: slurmv1.NodeContainer{AppArmorProfile: profile},
				SidecarSSSD: slurmv1.SidecarSSSD{
					Sssd: &slurmv1.NodeContainer{AppArmorProfile: profile},
				},
			}
			loginValues := buildSlurmLoginFrom("cluster", "", nil, login)
			require.Equal(t, profile, loginValues.ContainerSshd.AppArmorProfile)
			require.Equal(t, profile, loginValues.ContainerMunge.AppArmorProfile)
			require.Equal(t, profile, loginValues.ContainerSSSD.AppArmorProfile)

			nodeSet := &slurmv1alpha1.NodeSet{
				Spec: slurmv1alpha1.NodeSetSpec{
					Slurmd: slurmv1alpha1.ContainerSlurmdSpec{
						Security: slurmv1alpha1.ContainerSecuritySpec{AppArmorProfile: profile},
					},
					Munge: slurmv1alpha1.ContainerMungeSpec{
						Security: slurmv1alpha1.ContainerSecuritySpec{AppArmorProfile: profile},
					},
					SSSD: &slurmv1alpha1.ContainerSSSDSpec{
						Security: slurmv1alpha1.ContainerSecuritySpec{AppArmorProfile: profile},
					},
				},
			}
			nodeSetValues := BuildSlurmNodeSetFrom(nodeSet, "cluster", nil)
			require.Equal(t, profile, nodeSetValues.ContainerSlurmd.AppArmorProfile)
			require.Equal(t, profile, nodeSetValues.ContainerMunge.AppArmorProfile)
			require.Equal(t, profile, nodeSetValues.ContainerSSSD.AppArmorProfile)
		})
	}
}
