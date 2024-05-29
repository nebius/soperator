package k8smodels

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/models/k8s/naming"
)

type PVC struct {
	types.NamespacedName

	Size             resource.Quantity
	StorageClassName string
	AccessModes      []corev1.PersistentVolumeAccessMode
}

// TODO fix
type PV struct {
	types.NamespacedName

	Size             resource.Quantity
	StorageClassName string
	AccessModes      []corev1.PersistentVolumeAccessMode
}

func BuildPVCFrom(
	namespace,
	clusterName string,
	componentType consts.ComponentType,
	podSpec *slurmv1.PodSpec,
) (PVC, error) {
	size, storageClass, accessModes, err := pvcDefaultsFrom(componentType)
	if err != nil {
		return PVC{}, err
	}

	if podSpec != nil && podSpec.PVCSize != nil {
		size, err = resource.ParseQuantity(*podSpec.PVCSize)
		if err != nil {
			return PVC{}, err
		}
	}

	return PVC{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      k8snaming.BuildPVCName(clusterName, componentType),
		},
		Size:             size,
		StorageClassName: storageClass,
		AccessModes:      accessModes,
	}, nil

}

func pvcDefaultsFrom(
	componentType consts.ComponentType,
) (
	size resource.Quantity,
	storageClassName string,
	accessModes []corev1.PersistentVolumeAccessMode,
	err error,
) {
	switch componentType {
	case consts.ComponentTypeController:
		size = consts.DefaultPVCSizeController
		storageClassName = consts.DefaultPVCStorageClassController
		accessModes = consts.DefaultPVCAccessModesController
	case consts.ComponentTypeWorker:
		size = consts.DefaultPVCSizeWorker
		storageClassName = consts.DefaultPVCStorageClassWorker
		accessModes = consts.DefaultPVCAccessModesWorker
	default:
		err = fmt.Errorf("failed to get default pvc defaults for unknown component type %q", componentType)
	}

	return
}

func (p PVC) Render(clusterName string, component consts.ComponentType) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.Name,
			Namespace: p.Namespace,
			Labels:    BuildClusterDefaultLabels(clusterName, component),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: p.AccessModes,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: p.Size,
				},
			},
			StorageClassName: &p.StorageClassName,
		},
	}
}
