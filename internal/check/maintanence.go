package check

import "nebius.ai/slurm-operator/internal/consts"

func IsMaintenanceActive(maintenance *consts.MaintenanceMode) bool {
	return maintenance != nil && *maintenance != consts.ModeNone
}

func IsModeDownscaleAndDeletePopulate(maintenance *consts.MaintenanceMode) bool {
	return maintenance != nil && *maintenance == consts.ModeDownscaleAndDeletePopulate
}

func IsModeSkipPopulateJail(maintenance *consts.MaintenanceMode) bool {
	return maintenance != nil && *maintenance == consts.ModeSkipPopulateJail
}
