package framework

type WorkerPodRef struct {
	Name string
}

type ExpectedNodeSet struct {
	Name   string
	Size   int
	Preset string
	HasGPU bool
}

type ClusterState struct {
	Workers          []WorkerPodRef
	GPUWorkers       []WorkerPodRef
	WorkersByNodeSet map[string][]WorkerPodRef
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
	return len(s.GPUWorkers) > 0
}
