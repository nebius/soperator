package framework

type WorkerRef struct {
	Name string
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
