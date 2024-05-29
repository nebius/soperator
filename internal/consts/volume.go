package consts

const (
	VolumeSlurmKeyName     = SlurmPrefix + "key"
	VolumeSlurmConfigsName = SlurmPrefix + "configs"
	VolumeUsersName        = SlurmPrefix + "users"
	VolumeSpoolName        = SlurmPrefix + "spool"

	volumeSlurmK8sConfPath = "/etc/slurm-k8s-conf"

	VolumeSlurmKeyMountPath     = volumeSlurmK8sConfPath + "/key"
	VolumeSlurmConfigsMountPath = volumeSlurmK8sConfPath + "/configs"
	VolumeUsersMountPath        = "/etc/users"
	VolumeSpoolMountPath        = "/var/spool"
	VolumeJailMountPath         = "/mnt/jail"
)
