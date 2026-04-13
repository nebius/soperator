package framework

type WorkerRef struct {
	Name   string
	HasGPU bool
}

type ClusterState struct {
	Workers []WorkerRef
}

func (s *ClusterState) HasGPUWorkers() bool {
	for _, w := range s.Workers {
		if w.HasGPU {
			return true
		}
	}
	return false
}
