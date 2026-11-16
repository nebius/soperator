package soperatorchecks

import (
	"context"
	"testing"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/slurmapi"
)

func TestSlurmNodesController_recreatePoweredDownWorkers(t *testing.T) {
	slurmClusterName := types.NamespacedName{Namespace: "test-ns", Name: "test-cluster"}
	longAgo := time.Now().Add(-time.Hour)

	states := func(states ...api.V0044NodeState) map[api.V0044NodeState]struct{} {
		res := make(map[api.V0044NodeState]struct{}, len(states))
		for _, state := range states {
			res[state] = struct{}{}
		}
		return res
	}
	workerPod := func(mutate func(*corev1.Pod)) *corev1.Pod {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: slurmClusterName.Namespace,
				Name:      "worker-0",
				Labels:    map[string]string{consts.LabelWorkerKey: consts.LabelWorkerValue},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					Name: consts.ContainerNameSlurmd,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(longAgo)},
					},
				}},
			},
		}
		if mutate != nil {
			mutate(pod)
		}
		return pod
	}

	tests := []struct {
		name        string
		states      map[api.V0044NodeState]struct{}
		pod         *corev1.Pod
		wantDeleted bool
	}{
		{
			name:        "powered down node with old slurmd",
			states:      states(api.V0044NodeStateIDLE, api.V0044NodeStateCLOUD, api.V0044NodeStatePOWEREDDOWN),
			pod:         workerPod(nil),
			wantDeleted: true,
		},
		{
			name:        "down and powered down node after a resume failure",
			states:      states(api.V0044NodeStateDOWN, api.V0044NodeStateCLOUD, api.V0044NodeStatePOWEREDDOWN),
			pod:         workerPod(nil),
			wantDeleted: true,
		},
		{
			name:   "healthy node",
			states: states(api.V0044NodeStateIDLE, api.V0044NodeStateCLOUD),
			pod:    workerPod(nil),
		},
		{
			name:   "powering down node still ignores registrations",
			states: states(api.V0044NodeStateIDLE, api.V0044NodeStateCLOUD, api.V0044NodeStatePOWERINGDOWN),
			pod:    workerPod(nil),
		},
		{
			name:   "powering up node",
			states: states(api.V0044NodeStateIDLE, api.V0044NodeStateCLOUD, api.V0044NodeStatePOWEREDDOWN, api.V0044NodeStatePOWERINGUP),
			pod:    workerPod(nil),
		},
		{
			name:   "node rebooting for a rolling update",
			states: states(api.V0044NodeStateIDLE, api.V0044NodeStatePOWEREDDOWN, api.V0044NodeStateREBOOTISSUED),
			pod:    workerPod(nil),
		},
		{
			name:   "fresh slurmd has not registered yet",
			states: states(api.V0044NodeStateIDLE, api.V0044NodeStatePOWEREDDOWN),
			pod: workerPod(func(pod *corev1.Pod) {
				pod.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.Now()
			}),
		},
		{
			name:   "slurmd is not running",
			states: states(api.V0044NodeStateIDLE, api.V0044NodeStatePOWEREDDOWN),
			pod: workerPod(func(pod *corev1.Pod) {
				pod.Status.ContainerStatuses[0].State = corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
				}
			}),
		},
		{
			name:   "pod with the node name is not a worker",
			states: states(api.V0044NodeStateIDLE, api.V0044NodeStatePOWEREDDOWN),
			pod: workerPod(func(pod *corev1.Pod) {
				pod.Labels = nil
			}),
		},
		{
			name:   "suspended ephemeral node without a pod",
			states: states(api.V0044NodeStateIDLE, api.V0044NodeStateCLOUD, api.V0044NodeStatePOWEREDDOWN),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.pod != nil {
				builder = builder.WithObjects(tt.pod)
			}
			k8sClient := builder.Build()
			controller := &SlurmNodesController{Reconciler: reconciler.NewReconciler(k8sClient, scheme, nil)}

			err := controller.recreatePoweredDownWorkers(ctx, map[types.NamespacedName][]slurmapi.Node{
				slurmClusterName: {{Name: "worker-0", States: tt.states}},
			})
			require.NoError(t, err)

			if tt.pod == nil {
				return
			}
			err = k8sClient.Get(ctx, ctrlclient.ObjectKeyFromObject(tt.pod), &corev1.Pod{})
			require.Equal(t, tt.wantDeleted, apierrors.IsNotFound(err))
		})
	}
}
