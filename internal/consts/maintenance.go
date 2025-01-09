package consts

type MaintenanceMode string

const (
	ModeNone                          MaintenanceMode = "none"
	ModeDownscale                     MaintenanceMode = "downscale"
	ModeDownscaleAndDeletePopulate    MaintenanceMode = "downscaleAndDeletePopulateJail"
	ModeDownscaleAndOverwritePopulate MaintenanceMode = "downscaleAndOverwritePopulateJail"
	ModeSkipPopulate                  MaintenanceMode = "skipPopulateJail"
)

const (
	ZeroReplicas   = int32(0)
	SingleReplicas = int32(1)
)
