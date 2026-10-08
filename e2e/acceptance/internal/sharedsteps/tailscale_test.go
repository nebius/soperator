package sharedsteps

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

func TestTailscaleAuthenticationURLPresent(t *testing.T) {
	for _, tc := range []struct {
		name string
		logs string
		want bool
	}{
		{
			name: "ticket example",
			logs: "To authenticate, visit:\n\thttps://login.tailscale.com/a/bla_bla_bla\n",
			want: true,
		},
		{
			name: "timestamped prompt with noise",
			logs: "2026/10/08 startup\n2026/10/08 To authenticate, visit:\nhttps://login.tailscale.com/a/abc-123\nmore output",
			want: true,
		},
		{
			name: "URL before prompt",
			logs: "https://login.tailscale.com/a/abc\nTo authenticate, visit:\n",
		},
		{
			name: "missing prompt",
			logs: "https://login.tailscale.com/a/abc\n",
		},
		{
			name: "wrong scheme",
			logs: "To authenticate, visit:\nhttp://login.tailscale.com/a/abc\n",
		},
		{
			name: "wrong host",
			logs: "To authenticate, visit:\nhttps://example.com/a/abc\n",
		},
		{
			name: "empty token",
			logs: "To authenticate, visit:\nhttps://login.tailscale.com/a/\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tailscaleAuthenticationURLPresent(tc.logs))
		})
	}
}

func TestValidateTailscaleLoginPods(t *testing.T) {
	statefulSet := kubeobjects.LoginStatefulSet{}
	replicas := int32(2)
	statefulSet.Spec.Replicas = &replicas
	statefulSet.Status.ReadyReplicas = 2
	pods := []corev1.Pod{
		readyTailscalePod("login-1"),
		readyTailscalePod("login-0"),
	}

	names, err := validateTailscaleLoginPods(statefulSet, pods)

	require.NoError(t, err)
	assert.Equal(t, []string{"login-0", "login-1"}, names)
}

func TestValidateTailscaleLoginPodsRejectsMissingContainer(t *testing.T) {
	statefulSet := kubeobjects.LoginStatefulSet{}
	replicas := int32(1)
	statefulSet.Spec.Replicas = &replicas
	statefulSet.Status.ReadyReplicas = 1
	pod := readyTailscalePod("login-0")
	pod.Spec.InitContainers = nil

	_, err := validateTailscaleLoginPods(statefulSet, []corev1.Pod{pod})

	require.Error(t, err)
	assert.ErrorContains(t, err, "init container tailscale is missing")
}

func TestValidateTailscaleLoginPodsRejectsReplicaMismatch(t *testing.T) {
	statefulSet := kubeobjects.LoginStatefulSet{}
	replicas := int32(2)
	statefulSet.Spec.Replicas = &replicas
	statefulSet.Status.ReadyReplicas = 2

	_, err := validateTailscaleLoginPods(statefulSet, []corev1.Pod{readyTailscalePod("login-0")})

	require.Error(t, err)
	assert.ErrorContains(t, err, "got 1, expected 2")
}

func readyTailscalePod(name string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: tailscaleContainerName}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
}
