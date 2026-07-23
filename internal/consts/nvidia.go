package consts

const (
	EnvNvidiaGDRCopy = "NVIDIA_GDRCOPY"

	NvidiaIMEXCLIHostPath  = "/usr/bin/nvidia-imex-ctl"
	NvidiaIMEXCLIMountPath = NvidiaIMEXCLIHostPath
	NvidiaIMEXCLIJailPath  = VolumeMountPathJailUpper + NvidiaIMEXCLIHostPath

	NvidiaIMEXConfigHostPath  = "/etc/nvidia-imex"
	NvidiaIMEXConfigMountPath = NvidiaIMEXConfigHostPath
	NvidiaIMEXConfigJailPath  = VolumeMountPathJailUpper + NvidiaIMEXConfigHostPath
)
