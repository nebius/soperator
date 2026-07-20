package framework

type WorkerPodRef struct {
	Name string
}

type DiscoveredNodeSet struct {
	Name   string
	Size   int
	Preset string
	HasGPU bool
}

type ClusterState struct {
	SlurmClusterName   string
	Workers            []WorkerPodRef
	CPUWorkers         []WorkerPodRef
	GPUWorkers         []WorkerPodRef
	WorkersByNodeSet   map[string][]WorkerPodRef
	DiscoveredNodeSets []DiscoveredNodeSet
}

func (s *ClusterState) PodName(podName string) string {
	return ClusterPrefixedName(s.SlurmClusterName, podName)
}

func (s *ClusterState) DesiredWorkerCount() int {
	total := 0
	for _, nodeSet := range s.DiscoveredNodeSets {
		total += nodeSet.Size
	}
	return total
}

func (s *ClusterState) HasGPUWorkers() bool {
	return len(s.GPUWorkers) > 0
}

func (s *ClusterState) HasCPUWorkers() bool {
	return len(s.CPUWorkers) > 0
}

func (s *ClusterState) HasHeterogeneousWorkers() bool {
	return s.HasCPUWorkers() && s.HasGPUWorkers()
}
