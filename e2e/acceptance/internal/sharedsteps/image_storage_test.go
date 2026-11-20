package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestImageStorageDeepTreeExceedsPathLimitOnlyWhenJoined(t *testing.T) {
	building, joined := imageStorageDeepTreePathLengths()

	assert.Less(t, building, imageStoragePathMax, "each half must be creatable with a single path argument")
	assert.Greater(t, joined, imageStoragePathMax, "the joined tree must be longer than PATH_MAX")
	assert.LessOrEqual(t, imageStorageDeepSegmentLength, 255, "a path component must fit into NAME_MAX")
}

func TestImageStorageDeepTreeScriptCreatesMarkersWithExpectedModes(t *testing.T) {
	script := imageStorageContainerScript()

	assert.Contains(t, script, `mv "$root/deep/b" "$root/deep/a$half/b"`)
	assert.Contains(t, script, `install -m 4755 /dev/null "$root/setuid"`)
	assert.Contains(t, script, `install -m 0600 /dev/null "$root/private"`)
	assert.Contains(t, script, `install -d -m 1777 "$root/sticky"`)
	assert.Contains(t, script, `echo "created=now"`)
	assert.Contains(t, script, `echo "created=earlier"`)
	assert.NotContains(t, script, "%!", "format verbs must be fully resolved")
}

func TestParseImageStorageProbe(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    imageStorageProbe
		wantErr string
	}{
		{
			name:   "preserved content",
			output: "created=earlier\nmodes=4755 600 1777\nleaves=1\n",
			want:   imageStorageProbe{created: imageStorageContentCreatedEarlier, modes: imageStorageMarkerModes, leaves: 1},
		},
		{
			name:   "flattened permissions with shell noise",
			output: "Warning: Permanently added 'worker-0'\ncreated=now\n modes=777  777 777 \r\nleaves=1\n",
			want:   imageStorageProbe{created: imageStorageContentCreatedNow, modes: "777 777 777", leaves: 1},
		},
		{
			name:   "lost tree",
			output: "created=earlier\nmodes=4755 600 1777\nleaves=0\n",
			want:   imageStorageProbe{created: imageStorageContentCreatedEarlier, modes: imageStorageMarkerModes, leaves: 0},
		},
		{
			name:    "missing leaves",
			output:  "created=now\nmodes=4755 600 1777\n",
			wantErr: "created, modes or leaves are missing",
		},
		{
			name:    "invalid leaves",
			output:  "created=now\nmodes=4755 600 1777\nleaves=many\n",
			wantErr: "parse leaf count",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseImageStorageProbe(tt.output)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNodeSetHasImageStorage(t *testing.T) {
	imageStorage := framework.JailSubMountInfo{Name: containerRuntimeSubmountName, MountPath: "/mnt/image-storage"}
	other := framework.JailSubMountInfo{Name: "local-data", MountPath: "/mnt/local-data"}

	assert.True(t, nodeSetHasImageStorage(framework.NodeSetInfo{DockerEnabled: true, JailSubMounts: []framework.JailSubMountInfo{other, imageStorage}}))
	assert.False(t, nodeSetHasImageStorage(framework.NodeSetInfo{DockerEnabled: false, JailSubMounts: []framework.JailSubMountInfo{imageStorage}}))
	assert.False(t, nodeSetHasImageStorage(framework.NodeSetInfo{DockerEnabled: true, JailSubMounts: []framework.JailSubMountInfo{other}}))

	assert.False(t, anyNodeSetHasImageStorage(nil))
	assert.True(t, anyNodeSetHasImageStorage([]framework.NodeSetInfo{
		{DockerEnabled: true, JailSubMounts: []framework.JailSubMountInfo{other}},
		{DockerEnabled: true, JailSubMounts: []framework.JailSubMountInfo{imageStorage}},
	}))
}

func TestWorkerPodStartupDiagnostics(t *testing.T) {
	crashLooping := corev1.Pod{Status: corev1.PodStatus{
		Phase: corev1.PodPending,
		InitContainerStatuses: []corev1.ContainerStatus{
			{Name: "ensure-jail-virtiofs", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}},
			{
				Name:         "prepare-node-local-image-storage",
				RestartCount: 3,
				State:        corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			},
		},
	}}
	crashLooping.UID = "new"

	assert.True(t, imageStoragePrepareFailed(crashLooping))
	assert.Equal(t,
		"uid=new phase=Pending init/prepare-node-local-image-storage=waiting:CrashLoopBackOff restarts=3",
		describeWorkerPodStartup(crashLooping))

	starting := corev1.Pod{Status: corev1.PodStatus{
		Phase: corev1.PodPending,
		InitContainerStatuses: []corev1.ContainerStatus{
			{Name: "ensure-jail-virtiofs", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}},
			{Name: "prepare-node-local-image-storage", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
		},
	}}
	assert.False(t, imageStoragePrepareFailed(starting))

	managedFailedOnce := corev1.Pod{Status: corev1.PodStatus{InitContainerStatuses: []corev1.ContainerStatus{
		{Name: "prepare-node-local-image-storage-ext4", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}},
	}}}
	assert.True(t, imageStoragePrepareFailed(managedFailedOnce))

	otherInitRetrying := corev1.Pod{Status: corev1.PodStatus{InitContainerStatuses: []corev1.ContainerStatus{
		{Name: "ensure-jail-virtiofs", RestartCount: 2, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}},
	}}}
	assert.False(t, imageStoragePrepareFailed(otherInitRetrying), "mount checks may fail transiently")
}

func TestPodRecreatedAndReady(t *testing.T) {
	ready := corev1.Pod{Status: corev1.PodStatus{
		Phase:      corev1.PodRunning,
		Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
	}}
	ready.UID = "new"

	assert.True(t, podRecreatedAndReady(ready, "old"))
	assert.False(t, podRecreatedAndReady(ready, "new"), "the original pod must not count as recreated")

	notReady := ready
	notReady.Status.Conditions = nil
	assert.False(t, podRecreatedAndReady(notReady, "old"))
}
