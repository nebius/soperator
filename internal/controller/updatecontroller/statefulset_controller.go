/*
Copyright 2025 Nebius B.V.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package updatecontroller

import (
	"context"
	"fmt"
	"strings"
	"time"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/slurmapi"
)

const (
	labelToDelete = "kekus/todelete"
)

const (
	defaultSTSReplicasCount = int32(1)
)

type RollingUpdateReconciler struct {
	*reconciler.Reconciler

	slurmAPIClients *slurmapi.ClientSet
}

func NewRollingUpdateReconciler(
	client client.Client, scheme *runtime.Scheme,
	recorder record.EventRecorder,
	slurmAPIClients *slurmapi.ClientSet,
) *RollingUpdateReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)
	return &RollingUpdateReconciler{
		Reconciler:      r,
		slurmAPIClients: slurmAPIClients,
	}
}

// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;update;delete
//	+kubebuilder:rbac:groups=slurm.nebius.ai,resources=rollingupdatestate,verbs=get;list;watch;create;update;patch;delete
//	+kubebuilder:rbac:groups=slurm.nebius.ai,resources=rollingupdatestate/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.0/pkg/reconcile
func (r *RollingUpdateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("kekus")
	logger.Info("KEKUS")

	sts := &kruisev1b1.StatefulSet{}
	if err := r.Get(ctx, req.NamespacedName, sts); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	labels := sts.GetLabels()
	if labels[consts.LabelSoperatorRollingUpdateEnabled] != consts.LabelSoperatorRollingUpdateValue {
		return ctrl.Result{}, nil
	}

	clusterName, ok := labels[consts.LabelInstanceKey]
	if !ok || clusterName == "" {
		// missing
		return ctrl.Result{}, fmt.Errorf("missing cluster name in sts")
	}

	replicas := defaultSTSReplicasCount
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	if sts.Status.UpdatedReplicas == replicas {
		return ctrl.Result{}, nil
	}

	outdatedPodList, err := r.getOutdatedPodList(ctx, sts)
	if err != nil {
		return ctrl.Result{}, err
	}

	rollingUpdateState, err := r.ensureRollingUpdateState(ctx, sts, outdatedPodList)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.processRollingUpdate(ctx, clusterName, sts, outdatedPodList, rollingUpdateState); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *RollingUpdateReconciler) getOutdatedPodList(
	ctx context.Context,
	sts *kruisev1b1.StatefulSet,
) ([]corev1.Pod, error) {
	selector, err := metav1.LabelSelectorAsSelector(sts.Spec.Selector)
	if err != nil {
		return nil, err
	}

	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(sts.Namespace),
		client.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return nil, err
	}

	var res []corev1.Pod

	for _, pod := range podList.Items {
		pod := pod
		// TODO: REVISION
		podControllerRevisionHash := pod.Labels["controller-revision-hash"]
		if podControllerRevisionHash == sts.Status.UpdateRevision {
			continue
		}

		res = append(res, pod)
	}

	return res, nil
}

func (r *RollingUpdateReconciler) ensureRollingUpdateState(
	ctx context.Context,
	sts *kruisev1b1.StatefulSet,
	pods []corev1.Pod,
) (*v1alpha1.RollingUpdateState, error) {
	rollingUpdateState := &v1alpha1.RollingUpdateState{}

	err := r.Get(ctx, client.ObjectKey{
		Name:      sts.Name,
		Namespace: sts.Namespace,
	}, rollingUpdateState)
	if client.IgnoreNotFound(err) != nil {
		return nil, err
	}

	podNames := make([]string, 0, len(pods))
	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
	}

	rollingUpdateState.Spec.StatefulSetRef = sts.Name
	rollingUpdateState.Spec.RemainingPods = podNames
	rollingUpdateState.Status.RemainingPodsCount = len(podNames)

	err = r.Update(ctx, rollingUpdateState)
	if err != nil {
		return nil, err
	}

	if err := r.Status().Update(ctx, rollingUpdateState); err != nil {
		return nil, err
	}

	return rollingUpdateState, nil
}

func (r *RollingUpdateReconciler) processRollingUpdate(
	ctx context.Context,
	clusterName string,
	sts *kruisev1b1.StatefulSet,
	outdatedPods []corev1.Pod,
	rollingUpdateState *v1alpha1.RollingUpdateState,
) error {
	logger := log.FromContext(ctx).WithName("kekus-pekus")

	slurmNodesToReboot := []string{}
	for _, pod := range outdatedPods {
		if pod.Labels[labelToDelete] == "true" {
			if err := r.Delete(ctx, &pod); err != nil {
				return err
			}
			continue
		}

		slurmClient, ok := r.slurmAPIClients.GetClient(types.NamespacedName{
			Namespace: sts.Namespace,
			Name:      clusterName,
		})
		if !ok {
			logger.Info("no slurm clients", "namespace", sts.Namespace, "clusterName", clusterName)
			return fmt.Errorf("no slurm clients")
		}

		slurmNode, err := slurmClient.GetNode(ctx, pod.Name)
		if err != nil {
			return err
		}

		if slurmNode.IsRebootIssuedState() || slurmNode.IsRebootRequestedState() {
			continue
		}

		if pod.Labels[labelToDelete] != "True" {
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[labelToDelete] = "True"
			if err := r.Update(ctx, &pod); err != nil {
				return err
			}
		}

		slurmNodesToReboot = append(slurmNodesToReboot, slurmNode.Name)
	}

	rebooterCronJob := &batchv1.CronJob{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: sts.Namespace,
		Name:      "rebooter",
	}, rebooterCronJob)
	if err != nil {
		return err
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    rebooterCronJob.Namespace,
			GenerateName: rebooterCronJob.Name + "-",
			Labels:       rebooterCronJob.Spec.JobTemplate.Labels,
			Annotations:  rebooterCronJob.Spec.JobTemplate.Annotations,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(rebooterCronJob, batchv1.SchemeGroupVersion.WithKind("CronJob")),
			},
		},
		Spec: *rebooterCronJob.Spec.JobTemplate.Spec.DeepCopy(),
	}
	appendSlurmNodeEnv(job, slurmNodesToReboot)

	if err := r.Create(ctx, job); err != nil {
		return err
	}
	if err := r.waitForJobCompletion(ctx, client.ObjectKeyFromObject(job)); err != nil {
		return err
	}

	logger.Info("oki poki")

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RollingUpdateReconciler) SetupWithManager(
	mgr ctrl.Manager,
	maxConcurrency int,
	cacheSyncTimeout time.Duration,
) error {

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&kruisev1b1.StatefulSet{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(tce event.TypedCreateEvent[client.Object]) bool { return true },
			// TODO: update only in case of label added
			UpdateFunc:  func(tue event.TypedUpdateEvent[client.Object]) bool { return false },
			DeleteFunc:  func(tde event.TypedDeleteEvent[client.Object]) bool { return false },
			GenericFunc: func(tge event.TypedGenericEvent[client.Object]) bool { return false },
		})).
		Named("kekus").
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout))

	return controllerBuilder.Complete(r)
}

func appendSlurmNodeEnv(job *batchv1.Job, slurmNodes []string) {

	envVar := corev1.EnvVar{Name: "SLURM_NODES", Value: strings.Join(slurmNodes, ",")}

	for i := range job.Spec.Template.Spec.InitContainers {
		job.Spec.Template.Spec.InitContainers[i].Env = append(job.Spec.Template.Spec.InitContainers[i].Env, envVar)
	}
	for i := range job.Spec.Template.Spec.Containers {
		job.Spec.Template.Spec.Containers[i].Env = append(job.Spec.Template.Spec.Containers[i].Env, envVar)
	}
}

func (r *RollingUpdateReconciler) waitForJobCompletion(ctx context.Context, key client.ObjectKey) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		job := &batchv1.Job{}
		if err := r.Get(ctx, key, job); err != nil {
			return err
		}

		for _, condition := range job.Status.Conditions {
			if condition.Status != corev1.ConditionTrue {
				continue
			}
			switch condition.Type {
			case batchv1.JobComplete, batchv1.JobSuccessCriteriaMet:
				return nil
			case batchv1.JobFailed, batchv1.JobFailureTarget:
				return fmt.Errorf("job %s/%s failed: %s", job.Namespace, job.Name, condition.Message)
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
