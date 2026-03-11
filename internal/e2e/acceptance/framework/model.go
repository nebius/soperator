package framework

type WorkerRef struct {
	Name string
}

type ClusterState struct {
	Workers []WorkerRef
}

type InternalSSHConfig struct {
	UserName string
}

type SharedState struct {
	Cluster     ClusterState
	InternalSSH InternalSSHConfig
}

type ScenarioState struct {
	// Node replacement
	ReplacementWorker  WorkerRef
	OriginalInstanceID string
	MaintenanceJobID   string

	// Internal SSH
	TargetWorker WorkerRef
	SSHOutput    string

	// Package installation
	PackageWorker WorkerRef
}
