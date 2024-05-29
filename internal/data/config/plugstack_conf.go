package config

import (
	"fmt"

	"nebius.ai/slurm-operator/internal/consts"
)

func GenerateSpankConfig() ConfFile {
	res := rawConfig{}
	res.addLine(fmt.Sprintf("required chroot.so %s", consts.JailMountPath))
	return res
}
