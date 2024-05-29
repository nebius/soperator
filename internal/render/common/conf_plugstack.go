package common

import (
	"fmt"

	"nebius.ai/slurm-operator/internal/consts"
)

func GenerateSpankConfig() ConfFile {
	res := rawConfig{}
	res.addLine(fmt.Sprintf("required chroot.so %s", consts.VolumeJailMountPath))
	res.addLine("required spank_pyxis.so")
	return res
}
