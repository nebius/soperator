package framework

type WorkerRef struct {
	Name string
}

type ClusterState struct {
	Workers []WorkerRef
}
