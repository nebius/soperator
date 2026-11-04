package updatecontroller

import (
	"context"
	"crypto/sha256"
	"fmt"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	kruisev1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
)

func (r *RollingUpdateReconciler) reconcileWorkerRecovery(ctx context.Context, pod *corev1.Pod, node *slurmapi.Node) (bool, error) {
	requestName := pod.Annotations[consts.AnnotationSoperatorWorkerRecoveryRequest]
	targetID := pod.Annotations[consts.AnnotationSoperatorWorkerRecoveryContainerID]
	registered := !node.IsRebootRequestedState() && !node.IsRebootIssuedState() && workerRecoveryRegistered(node)
	if requestName == "" {
		if registered {
			return true, nil
		}
		if !node.IsRebootIssuedState() {
			return false, nil
		}
		if err := validateWorkerRecovery(node); err != nil {
			return false, err
		}
		targetID = workerSlurmdContainerID(pod)
		if targetID == "" || pod.UID == "" {
			return false, fmt.Errorf("read pod and slurmd container identity for worker %s/%s", pod.Namespace, pod.Name)
		}
		// The pre-transition version distinguishes repeated recovery of one revision.
		sum := sha256.Sum256([]byte(string(pod.UID) + ":" + pod.Labels[consts.LabelSoperatorWorkerOperationID] + ":" + pod.ResourceVersion))
		requestName = fmt.Sprintf("worker-recovery-%x", sum[:16])
		patchBase := pod.DeepCopy()
		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string)
		}
		pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseRecovering
		pod.Annotations[consts.AnnotationSoperatorWorkerRecoveryRequest] = requestName
		pod.Annotations[consts.AnnotationSoperatorWorkerRecoveryContainerID] = targetID
		if err := r.Patch(ctx, pod, client.StrategicMergeFrom(patchBase, client.MergeFromWithOptimisticLock{})); err != nil {
			return false, fmt.Errorf("record worker recovery on pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
	}
	if targetID == "" {
		return false, fmt.Errorf("read recovery container identity for worker %s/%s", pod.Namespace, pod.Name)
	}
	request := &kruisev1alpha1.ContainerRecreateRequest{}
	err := r.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: requestName}, request)
	if err == nil {
		if !metav1.IsControlledBy(request, pod) || request.Spec.PodName != pod.Name ||
			request.Labels[kruisev1alpha1.ContainerRecreateRequestPodUIDKey] != string(pod.UID) ||
			len(request.Spec.Containers) != 1 || request.Spec.Containers[0].Name != consts.ContainerNameSlurmd ||
			request.Spec.Containers[0].StatusContext == nil || request.Spec.Containers[0].StatusContext.ContainerID != targetID {
			return false, fmt.Errorf("verify worker recovery request %s/%s identity", request.Namespace, request.Name)
		}
		if request.Status.Phase != kruisev1alpha1.ContainerRecreateRequestCompleted {
			return false, nil
		}
		for _, state := range request.Status.ContainerRecreateStates {
			if state.Name == consts.ContainerNameSlurmd && state.Phase == kruisev1alpha1.ContainerRecreateRequestSucceeded {
				return registered && workerSlurmdContainerID(pod) != "" && workerSlurmdContainerID(pod) != targetID, nil
			}
		}
		return false, fmt.Errorf("complete worker recovery request %s/%s: %s; container states: %v", request.Namespace, request.Name, request.Status.Message, request.Status.ContainerRecreateStates)
	}
	if !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("read worker recovery request %s/%s: %w", pod.Namespace, requestName, err)
	}
	currentID := workerSlurmdContainerID(pod)
	if currentID != "" && currentID != targetID {
		return registered, nil
	}
	if err := validateWorkerRecovery(node); err != nil {
		return false, err
	}
	request = &kruisev1alpha1.ContainerRecreateRequest{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pod.Namespace, Name: requestName,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(pod, corev1.SchemeGroupVersion.WithKind("Pod"))},
		},
		Spec: kruisev1alpha1.ContainerRecreateRequestSpec{
			PodName:               pod.Name,
			Containers:            []kruisev1alpha1.ContainerRecreateRequestContainer{{Name: consts.ContainerNameSlurmd}},
			Strategy:              &kruisev1alpha1.ContainerRecreateRequestStrategy{FailurePolicy: kruisev1alpha1.ContainerRecreateRequestFailurePolicyFail},
			ActiveDeadlineSeconds: ptr.To(int64(300)),
		},
	}
	// Keep the request until Pod garbage collection so a lost Create response is
	// retried with the same request, including after an operator restart.
	if err := r.Create(ctx, request); err != nil && !apierrors.IsAlreadyExists(err) {
		return false, fmt.Errorf("create worker recovery request %s/%s: %w", pod.Namespace, requestName, err)
	}
	return false, nil
}

func validateWorkerRecovery(node *slurmapi.Node) error {
	if !node.IsDrainState() || !node.IsRebootIssuedState() || node.IsRebootRequestedState() ||
		len(node.NextStateAfterReboot) != 1 || node.NextStateAfterReboot[0] != api.V0044NodeNextStateAfterRebootINVALID {
		return fmt.Errorf("recover worker %s: issued reboot does not preserve drain", node.Name)
	}
	cpus, known := node.CPUAllocated()
	if !known || cpus != 0 || node.AllocMemoryMB == nil || *node.AllocMemoryMB != 0 || node.IsCompletingState() {
		return fmt.Errorf("recover worker %s: wait for zero known allocations", node.Name)
	}
	return nil
}

func workerSlurmdContainerID(pod *corev1.Pod) string {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == consts.ContainerNameSlurmd {
			return status.ContainerID
		}
	}
	return ""
}
