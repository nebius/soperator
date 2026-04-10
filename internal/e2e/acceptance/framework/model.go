package framework

type WorkerRef struct {
	Name   string
	HasGPU bool
}

type ExpectedNodeSet struct {
	Name   string
	Size   int
	Preset string
	HasGPU bool
}

type ClusterState struct {
	Workers          []WorkerRef
	GPUWorkers       []WorkerRef
	WorkersByNodeSet map[string][]WorkerRef
	ExpectedNodeSets []ExpectedNodeSet
}

func (s *ClusterState) ExpectedWorkerCount() int {
	total := 0
	for _, nodeSet := range s.ExpectedNodeSets {
		total += nodeSet.Size
	}
	return total
}

func (s *ClusterState) HasGPUWorkers() bool {
	if len(s.GPUWorkers) > 0 {
		return true
	}
	for _, w := range s.Workers {
		if w.HasGPU {
			return true
		}
	}
	return false
}
