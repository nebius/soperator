package sharedsteps

import (
	"testing"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestPickDockerWorkers(t *testing.T) {
	for _, workerType := range []string{"CPU", "GPU"} {
		t.Run(workerType, func(t *testing.T) {
			snapshot := dockerWorkerSnapshotForTest()
			workers, err := pickDockerWorkers(snapshot, workerType, 2)
			require.NoError(t, err)
			for _, worker := range workers {
				assert.Equal(t, workerType == "GPU", worker.HasGPU)
				assert.NotContains(t, worker.NodeSetName, "disabled")
			}
			assert.Len(t, workers, 2)
			_, err = pickDockerWorkers(snapshot, workerType, 3)
			assert.True(t, framework.IsInsufficientWorkers(err))
		})
	}
}

func TestPickDockerWorkersSkipsUnconfiguredCapacity(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*framework.WorkerSnapshot)
	}{
		{name: "absent kind", prepare: func(snapshot *framework.WorkerSnapshot) {
			snapshot.NodeSets = snapshot.NodeSets[1:]
		}},
		{name: "Docker disabled", prepare: func(snapshot *framework.WorkerSnapshot) {
			snapshot.NodeSets[0].DockerEnabled = false
		}},
		{name: "too few configured", prepare: func(snapshot *framework.WorkerSnapshot) {
			snapshot.NodeSets[0].Replicas = 1
			snapshot.WorkersByNodeSet["cpu"] = snapshot.WorkersByNodeSet["cpu"][:1]
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := dockerWorkerSnapshotForTest()
			test.prepare(&snapshot)
			_, err := pickDockerWorkers(snapshot, "CPU", 2)
			require.Error(t, err)
			assert.ErrorIs(t, framework.SkipIfInsufficientWorkers(nil, err), godog.ErrSkip)
		})
	}
	_, err := pickDockerWorkers(framework.WorkerSnapshot{}, "GPU", 1)
	assert.ErrorIs(t, framework.SkipIfInsufficientWorkers(nil, err), godog.ErrSkip)
}

func TestPickDockerWorkersFailsWhenConfiguredWorkersAreUnavailable(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*framework.WorkerSnapshot)
	}{
		{name: "missing", prepare: func(snapshot *framework.WorkerSnapshot) {
			snapshot.WorkersByNodeSet["gpu"] = snapshot.WorkersByNodeSet["gpu"][:1]
		}},
		{name: "unhealthy", prepare: func(snapshot *framework.WorkerSnapshot) {
			snapshot.WorkersByNodeSet["gpu"][1].SlurmNode.State = "IDLE+DRAIN"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := dockerWorkerSnapshotForTest()
			test.prepare(&snapshot)
			_, err := pickDockerWorkers(snapshot, "GPU", 2)
			require.Error(t, err)
			assert.False(t, framework.IsInsufficientWorkers(err))
			assert.ErrorContains(t, err, "found 1 usable GPU workers, need 2")
			_, err = pickDockerWorkers(snapshot, "CPU", 2)
			assert.NoError(t, err)
		})
	}
}

func dockerWorkerSnapshotForTest() framework.WorkerSnapshot {
	snapshot := framework.WorkerSnapshot{
		NodeSets: []framework.NodeSetInfo{
			{Name: "cpu", Replicas: 2, DockerEnabled: true},
			{Name: "gpu", Replicas: 2, DockerEnabled: true, HasGPU: true},
			{Name: "disabled", Replicas: 2},
			{Name: "disabled-gpu", Replicas: 2, HasGPU: true},
		},
		WorkersByNodeSet: make(map[string][]framework.WorkerInfo),
	}
	for _, nodeSet := range snapshot.NodeSets {
		for _, suffix := range []string{"-0", "-1"} {
			snapshot.WorkersByNodeSet[nodeSet.Name] = append(snapshot.WorkersByNodeSet[nodeSet.Name], framework.WorkerInfo{
				Name:        nodeSet.Name + suffix,
				NodeSetName: nodeSet.Name,
				HasGPU:      nodeSet.HasGPU,
				SlurmNode:   framework.SlurmNodeInfo{State: "IDLE"},
			})
		}
	}
	return snapshot
}

func TestRuntimePathsUnderAcceptsDockerInspectPaths(t *testing.T) {
	const root = "/mnt/image-storage/docker"
	output := `
/mnt/image-storage/docker/containers/ac9f51a97037/resolv.conf
/mnt/image-storage/docker/containers/ac9f51a97037/hostname
/mnt/image-storage/docker/containers/ac9f51a97037/hosts
/mnt/image-storage/docker/containers/ac9f51a97037/ac9f51a97037-json.log
`

	if !runtimePathsUnder(output, root) {
		t.Fatalf("runtimePathsUnder() = false, want true")
	}
}

func TestRuntimePathsUnderAcceptsHostVisibleJailPaths(t *testing.T) {
	const root = "/mnt/local-data/docker"
	output := `/mnt/jail/mnt/local-data/docker/containers/ac9f51a97037/resolv.conf
/mnt/jail/mnt/local-data/docker/containers/ac9f51a97037/hostname`

	if !runtimePathsUnder(output, root) {
		t.Fatalf("runtimePathsUnder() = false, want true")
	}
}

func TestRuntimePathsUnderRejectsUnexpectedPaths(t *testing.T) {
	const root = "/mnt/image-storage/docker"
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "metadata without paths",
			output: "7a233c2a9c9881f79238b610bcfbe8b7bcd014a43b8c1dc17fcab60fa3e61c4c",
			want:   false,
		},
		{
			name:   "path outside root",
			output: "/var/lib/docker/containers/ac9f51a97037/hosts",
			want:   false,
		},
		{
			name: "one runtime path outside root",
			output: `/mnt/image-storage/docker/containers/ac9f51a97037/hosts
/var/lib/docker/containers/ac9f51a97037/hostname`,
			want: false,
		},
		{
			name:   "path under root",
			output: "/mnt/image-storage/docker/containers/ac9f51a97037/hosts",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtimePathsUnder(tt.output, root); got != tt.want {
				t.Fatalf("runtimePathsUnder() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestPathIsUnder(t *testing.T) {
	const root = "/mnt/image-storage/docker"
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "root", value: "/mnt/image-storage/docker", want: true},
		{name: "child", value: "/mnt/image-storage/docker/overlay2/container/merged", want: true},
		{name: "sibling with common prefix", value: "/mnt/image-storage/docker-other", want: false},
		{name: "outside root", value: "/var/lib/docker", want: false},
		{name: "cleaned outside root", value: "/mnt/image-storage/docker/../containerd", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathIsUnder(tt.value, root); got != tt.want {
				t.Fatalf("pathIsUnder(%q, %q) = %t, want %t", tt.value, root, got, tt.want)
			}
		})
	}
}

func TestDockerJailVisiblePath(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "jail visible", value: "/mnt/image-storage/docker", want: "/mnt/image-storage/docker"},
		{name: "host visible", value: "/mnt/jail/mnt/image-storage/docker", want: "/mnt/image-storage/docker"},
		{name: "jail root", value: "/mnt/jail", want: "/"},
		{name: "similar prefix", value: "/mnt/jail-other/docker", want: "/mnt/jail-other/docker"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dockerJailVisiblePath(tt.value)
			if err != nil {
				t.Fatalf("dockerJailVisiblePath() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("dockerJailVisiblePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDockerJailVisiblePathRejectsTraversal(t *testing.T) {
	if _, err := dockerJailVisiblePath("/mnt/jail/mnt/image-storage/../docker"); err == nil {
		t.Fatal("dockerJailVisiblePath() error = nil, want error")
	}
}

func TestDockerCgroupParentBelongsToJob(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		jobID  string
		wanted bool
	}{
		{
			name:   "Slurm job user cgroup",
			value:  "/kubepods.slice/pod.scope/slurm/uid_1000/job_42/step_0/user",
			jobID:  "42",
			wanted: true,
		},
		{
			name:   "Slurm 26.05 SLUID user cgroup",
			value:  "/kubepods.slice/pod.scope/system.slice/slurmstepd.scope/s8G5M22WGXB100/step_0/user",
			jobID:  "42",
			wanted: true,
		},
		{
			name:   "different Slurm job",
			value:  "/kubepods.slice/pod.scope/slurm/uid_1000/job_43/step_0/user",
			jobID:  "42",
			wanted: false,
		},
		{
			name:   "task cgroup instead of user parent",
			value:  "/kubepods.slice/pod.scope/slurm/uid_1000/job_42/step_0/user/task_0",
			jobID:  "42",
			wanted: false,
		},
		{
			name:   "SLUID outside slurmstepd scope",
			value:  "/kubepods.slice/pod.scope/other.scope/s8G5M22WGXB100/step_0/user",
			jobID:  "42",
			wanted: false,
		},
		{
			name:   "malformed SLUID",
			value:  "/kubepods.slice/pod.scope/system.slice/slurmstepd.scope/s8G5M22WGXB10O/step_0/user",
			jobID:  "42",
			wanted: false,
		},
		{
			name:   "unknown step",
			value:  "/kubepods.slice/pod.scope/system.slice/slurmstepd.scope/s8G5M22WGXB100/step_other/user",
			jobID:  "42",
			wanted: false,
		},
		{
			name:   "missing job ID",
			value:  "/kubepods.slice/pod.scope/system.slice/slurmstepd.scope/s8G5M22WGXB100/step_0/user",
			jobID:  "",
			wanted: false,
		},
		{
			name:   "dockerd default",
			value:  "",
			jobID:  "42",
			wanted: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := dockerCgroupParentBelongsToJob(test.value, test.jobID); got != test.wanted {
				t.Fatalf("dockerCgroupParentBelongsToJob(%q, %q) = %t, want %t", test.value, test.jobID, got, test.wanted)
			}
		})
	}
}
