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
