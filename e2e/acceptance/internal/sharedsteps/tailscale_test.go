package sharedsteps

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/assert"
)

func TestValidateUnauthenticatedTailscaleStatus(t *testing.T) {
	for name, test := range map[string]struct {
		status  tailscaleStatus
		wantErr string
	}{
		"valid": {
			status: tailscaleStatus{
				TUN:          true,
				BackendState: "NeedsLogin",
				AuthURL:      "https://login.tailscale.com/a/example",
			},
		},
		"wrong backend state": {
			status:  tailscaleStatus{TUN: true, BackendState: "Running", AuthURL: "https://login.tailscale.com/a/example"},
			wantErr: "expected NeedsLogin",
		},
		"missing auth URL": {
			status:  tailscaleStatus{TUN: true, BackendState: "NeedsLogin"},
			wantErr: "AuthURL is empty",
		},
		"userspace networking": {
			status:  tailscaleStatus{BackendState: "NeedsLogin", AuthURL: "https://login.tailscale.com/a/example"},
			wantErr: "not using the kernel TUN device",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateUnauthenticatedTailscaleStatus(test.status)
			if test.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestValidateTailscaleSidecar(t *testing.T) {
	restartAlways := corev1.ContainerRestartPolicyAlways
	for name, test := range map[string]struct {
		pod     corev1.Pod
		wantErr string
	}{
		"valid": {
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "login-0"},
				Spec: corev1.PodSpec{InitContainers: []corev1.Container{{
					Name:          tailscaleContainerName,
					RestartPolicy: &restartAlways,
				}}},
			},
		},
		"missing": {
			pod:     corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "login-0"}},
			wantErr: "has no Tailscale init container",
		},
		"not restartable": {
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "login-0"},
				Spec: corev1.PodSpec{InitContainers: []corev1.Container{{
					Name: tailscaleContainerName,
				}}},
			},
			wantErr: "does not use restartPolicy=Always",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateTailscaleSidecar(test.pod)
			if test.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, test.wantErr)
		})
	}
}
