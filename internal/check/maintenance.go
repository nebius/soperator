package check

import (
	corev1 "k8s.io/api/core/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

func IsMaintenanceActive(maintenance *consts.MaintenanceMode) bool {
	return maintenance != nil && *maintenance != consts.ModeNone && *maintenance != consts.ModeSkipPopulate
}

func IsModeDownscaleAndDeletePopulate(maintenance *consts.MaintenanceMode) bool {
	return maintenance != nil && *maintenance == consts.ModeDownscaleAndDeletePopulate
}

func IsModeDownscaleAndOverwritePopulate(maintenance *consts.MaintenanceMode) bool {
	return maintenance != nil && *maintenance == consts.ModeDownscaleAndOverwritePopulate
}

func IsModeSkipPopulateJail(maintenance *consts.MaintenanceMode) bool {
	return maintenance != nil && *maintenance == consts.ModeSkipPopulate
}

func IsConditionFalseOrEmpty(condition corev1.NodeCondition) bool {
	return condition == (corev1.NodeCondition{}) || condition.Status == corev1.ConditionFalse
}

func IsConditionTrue(condition corev1.NodeCondition) bool {
	return condition != (corev1.NodeCondition{}) && condition.Status == corev1.ConditionTrue
}
