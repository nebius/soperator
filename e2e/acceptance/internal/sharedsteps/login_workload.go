package sharedsteps

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	loginStatefulSetResource = "statefulsets.apps.kruise.io"
	workloadInstanceLabel    = "app.kubernetes.io/instance"
	workloadComponentLabel   = "app.kubernetes.io/component"
	loginComponent           = "login"
)

type loginWorkload struct {
	clusterName string
	kubectl     *framework.KubectlClient
}

func newLoginWorkload(clusterName string, kubectl *framework.KubectlClient) *loginWorkload {
	return &loginWorkload{
		clusterName: clusterName,
		kubectl:     kubectl,
	}
}

func (w *loginWorkload) statefulSet(ctx context.Context) (kubeobjects.LoginStatefulSet, error) {
	var statefulSets kubeobjects.LoginStatefulSetList
	if err := w.kubectl.GetJSON(ctx, &statefulSets,
		"get", loginStatefulSetResource,
		"-n", framework.SoperatorNamespace,
		"-l", w.selector(), "-o", "json",
	); err != nil {
		return kubeobjects.LoginStatefulSet{}, fmt.Errorf("list login StatefulSets: %w", err)
	}
	if len(statefulSets.Items) != 1 {
		return kubeobjects.LoginStatefulSet{}, fmt.Errorf("find login StatefulSet: got %d, expected 1", len(statefulSets.Items))
	}
	return statefulSets.Items[0], nil
}

func (w *loginWorkload) pods(ctx context.Context) ([]corev1.Pod, error) {
	var pods corev1.PodList
	if err := w.kubectl.GetJSON(ctx, &pods,
		"get", "pods", "-n", framework.SoperatorNamespace,
		"-l", w.selector(), "-o", "json",
	); err != nil {
		return nil, fmt.Errorf("list login pods: %w", err)
	}
	return pods.Items, nil
}

func (w *loginWorkload) selector() string {
	return fmt.Sprintf("%s=%s,%s=%s",
		workloadInstanceLabel, w.clusterName,
		workloadComponentLabel, loginComponent,
	)
}
