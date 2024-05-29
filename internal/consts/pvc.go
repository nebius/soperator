package consts

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	PVCControllerSpoolName = ComponentNameController + "spool"

	DefaultPVCStorageClassController = "nebius-network-ssd"
	DefaultPVCStorageClassWorker     = "nebius-network-ssd"
)

var (
	DefaultPVCSizeController        = resource.MustParse("30Gi")
	DefaultPVCSizeWorker            = resource.MustParse("20Gi")
	DefaultPVCAccessModesController = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	DefaultPVCAccessModesWorker     = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
)
