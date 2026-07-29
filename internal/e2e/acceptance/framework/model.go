package framework

type WorkerRef struct {
	Name    string
	PodName string
}

type DiscoveredNodeSet struct {
	Name   string
	Size   int
	HasGPU bool
}

type ClusterState struct {
	SlurmClusterName   string
	Workers            []WorkerRef
	CPUWorkers         []WorkerRef
	GPUWorkers         []WorkerRef
	WorkersByNodeSet   map[string][]WorkerRef
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

func (s *ClusterState) IsHeterogeneousCluster() bool {
	return s.HasCPUWorkers() && s.HasGPUWorkers()
}

func WorkerNames(workers []WorkerRef) []string {
	names := make([]string, 0, len(workers))
	for _, worker := range workers {
		names = append(names, worker.Name)
	}
	return names
}
