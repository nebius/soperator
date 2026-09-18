package framework

type WorkerInfo struct {
	Name        string
	NodeSetName string
	HasGPU      bool
	SlurmNode   SlurmNodeInfo
}

func (w WorkerInfo) IsUsable() bool {
	return w.SlurmNode.IsUsable()
}

// ClusterInfo holds static runner metadata. It intentionally contains no
// discovered cluster topology; steps should query live state at scenario time.
type ClusterInfo struct {
	SlurmClusterName       string
	TargetSoperatorVersion string
	WorkloadNamePrefix     string
}

func (s *ClusterInfo) PodName(podName string) string {
	return SoperatorPodName(s.SlurmClusterName, s.TargetSoperatorVersion, s.WorkloadNamePrefix, podName)
}

func SoperatorPodName(slurmClusterName, soperatorVersion, workloadNamePrefix, podName string) string {
	if workloadNamePrefix == "disabled" || (workloadNamePrefix == "" && SoperatorVersionBeforeFive(soperatorVersion)) {
		return podName
	}

	return ClusterPrefixedName(slurmClusterName, podName)
}

func WorkerNames(workers []WorkerInfo) []string {
	names := make([]string, 0, len(workers))
	for _, worker := range workers {
		names = append(names, worker.Name)
	}
	return names
}
