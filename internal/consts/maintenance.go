package consts

type MaintenanceMode string

const (
	ModeNone                       MaintenanceMode = "none"
	ModeDownscale                  MaintenanceMode = "downscale"
	ModeDownscaleAndDeletePopulate MaintenanceMode = "downscaleAndDeletePopulateJail"
	ModeSkipPopulateJail           MaintenanceMode = "skipPopulateJail"
)

const (
	ZeroReplicas   = int32(0)
	SingleReplicas = int32(1)
)
