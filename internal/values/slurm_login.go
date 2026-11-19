package values

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

// SlurmLogin contains the data needed to deploy and reconcile Slurm Login nodes
type SlurmLogin struct {
	slurmv1.SlurmNode

	ContainerSshd        Container
	ContainerMunge       Container
	ContainerSSSD        *Container
	CustomInitContainers []corev1.Container

	Service         Service
	HeadlessService Service
	StatefulSet     StatefulSet
	UpdateStrategy  consts.UpdateStrategy

	IsSSHDConfigMapDefault  bool
	SSHDConfigMapName       string
	IsSSSDSecretDefault     bool
	SSSDConfSecretName      string
	SSSDLdapCAConfigMapName string
	SSHRootPublicKeys       []string

	VolumeJail         slurmv1.NodeVolume
	JailSubMounts      []slurmv1.NodeVolumeMount
	CustomVolumeMounts []slurmv1.NodeVolumeMount

	UserIsolation *slurmv1.LoginUserIsolation
	Autoscaling   *slurmv1.LoginAutoscaling
	DockerEnabled bool

	Maintenance *consts.MaintenanceMode
}

func buildSlurmLoginFrom(clusterName, namePrefix string, maintenance *consts.MaintenanceMode, login *slurmv1.SlurmNodeLogin) SlurmLogin {
	svc := buildServiceFrom(naming.BuildServiceName(consts.ComponentTypeLogin, clusterName))
	svc.Type = login.SshdServiceType
	svc.Annotations = login.SshdServiceAnnotations
	svc.LoadBalancerIP = login.SshdServiceLoadBalancerIP
	svc.LoadBalancerSourceRanges = login.SshdServiceLoadBalancerSourceRanges
	svc.NodePort = login.SshdServiceNodePort

	headlessSvc := buildServiceFrom(naming.BuildLoginHeadlessServiceName(clusterName))
	headlessSvc.Type = corev1.ServiceTypeClusterIP

	sshdConfigMapName := login.SSHDConfigMapRefName
	isSSHDConfigDefault := sshdConfigMapName == ""
	if isSSHDConfigDefault {
		sshdConfigMapName = naming.BuildConfigMapSSHDConfigsNameLogin(clusterName)
	}

	sssdConfSecretName := login.SSSDConfSecretRefName
	isSSSDSecretDefault := sssdConfSecretName == ""
	if isSSSDSecretDefault {
		sssdConfSecretName = naming.BuildSecretSSSDConfName(clusterName)
	}

	updateStrategy := login.UpdateStrategy
	if updateStrategy == "" {
		updateStrategy = consts.UpdateStrategyDelayedRollout
	}

	res := SlurmLogin{
		SlurmNode: *login.SlurmNode.DeepCopy(),
		ContainerSshd: buildContainerFrom(
			login.Sshd,
			consts.ContainerNameSshd,
		),
		ContainerMunge: buildContainerFrom(
			login.Munge,
			consts.ContainerNameMunge,
		),
		CustomInitContainers: login.CustomInitContainers,
		UpdateStrategy:       updateStrategy,
		Service:              svc,
		HeadlessService:      headlessSvc,
		StatefulSet: buildStatefulSetFrom(
			naming.BuildStatefulSetName(consts.ComponentTypeLogin, namePrefix),
			login.SlurmNode.Size,
		),
		SSHDConfigMapName:       sshdConfigMapName,
		IsSSHDConfigMapDefault:  isSSHDConfigDefault,
		SSSDConfSecretName:      sssdConfSecretName,
		SSSDLdapCAConfigMapName: login.SSSDLdapCAConfigMapRefName,
		IsSSSDSecretDefault:     isSSSDSecretDefault,
		SSHRootPublicKeys:       login.SshRootPublicKeys,
		VolumeJail:              *login.Volumes.Jail.DeepCopy(),
		Maintenance:             maintenance,
		UserIsolation:           login.UserIsolation.DeepCopy(),
		Autoscaling:             login.Autoscaling.DeepCopy(),
		DockerEnabled:           login.Docker != nil && ptr.Deref(login.Docker.Enabled, false),
	}
	if login.Sssd != nil {
		containerSSSD := buildContainerFrom(
			*login.Sssd,
			consts.ContainerNameSSSD,
		)
		containerSSSD.SSSDDebugLevel = login.SSSDDebugLevel
		res.ContainerSSSD = &containerSSSD
	}
	for _, jailSubMount := range login.Volumes.JailSubMounts {
		subMount := *jailSubMount.DeepCopy()
		res.JailSubMounts = append(res.JailSubMounts, subMount)
	}
	for _, customVolumeMount := range login.Volumes.CustomMounts {
		customMount := *customVolumeMount.DeepCopy()
		res.CustomVolumeMounts = append(res.CustomVolumeMounts, customMount)
	}

	return res
}

func (l SlurmLogin) IsAutoscalingEnabled() bool {
	return l.Autoscaling != nil && l.Autoscaling.Enabled
}

// ResolveLoginUpdatePolicy defaults the notice and validates an optional cron schedule.
// An omitted schedule returns nil; an explicitly configured zero notice is preserved.
func ResolveLoginUpdatePolicy(config *slurmv1.LoginUpdatePolicy) (time.Duration, cron.Schedule, error) {
	minNotice := consts.DefaultLoginUpdateMinNotice
	if config != nil && config.MinNotice != nil {
		minNotice = config.MinNotice.Duration
	}
	if minNotice < 0 {
		return 0, nil, fmt.Errorf("configure login update notice: duration must not be negative")
	}

	if config == nil || config.Schedule == nil {
		return minNotice, nil, nil
	}

	spec := strings.TrimSpace(*config.Schedule)
	if strings.HasPrefix(spec, "CRON_TZ=") || strings.HasPrefix(spec, "TZ=") {
		// robfig/cron v3.0.1 slices the timezone before validating its separator.
		zone, _, separated := strings.Cut(spec, " ")
		if !separated || strings.HasSuffix(zone, "=") {
			return 0, nil, fmt.Errorf("provide a nonempty cron timezone followed by five cron fields")
		}
	}
	schedule, err := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(spec)
	if err != nil {
		return 0, nil, fmt.Errorf("parse login update schedule: %w", err)
	}
	return minNotice, schedule, nil
}
