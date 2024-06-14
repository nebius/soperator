package benchmark

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

func RenderCronJob(
	namespace,
	clusterName string,
	nodeFilters []slurmv1.K8sNodeFilter,
	secrets *slurmv1.Secrets,
	volumeSources []slurmv1.VolumeSource,
	ncclBenchmark *values.SlurmNCCLBenchmark,
) (batchv1.CronJob, error) {
	labels := common.RenderLabels(consts.ComponentTypeBenchmark, clusterName)

	nodeFilter := utils.MustGetBy(
		nodeFilters,
		ncclBenchmark.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)

	cj := batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      consts.ContainerNameNCCLBenchmark,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: batchv1.CronJobSpec{
			Schedule: ncclBenchmark.Schedule,
			Suspend:  ptr.To(!ncclBenchmark.Enabled), // Suspend param in CronJob is inverted value Enabled from CRD
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Parallelism:  ptr.To(int32(1)),
					Completions:  ptr.To(int32(1)),
					BackoffLimit: ptr.To(int32(0)),
				},
			},
			SuccessfulJobsHistoryLimit: &ncclBenchmark.SuccessfulJobsHistoryLimit,
			FailedJobsHistoryLimit:     &ncclBenchmark.FailedJobsHistoryLimit,
		},
	}
	pts := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labels,
			Annotations: map[string]string{
				fmt.Sprintf(
					"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameNCCLBenchmark,
				): consts.AnnotationApparmorValueUnconfined,
			},
		},
		Spec: corev1.PodSpec{
			Affinity:              nodeFilter.Affinity,
			NodeSelector:          nodeFilter.NodeSelector,
			Tolerations:           nodeFilter.Tolerations,
			ActiveDeadlineSeconds: &ncclBenchmark.ActiveDeadlineSeconds,
			RestartPolicy:         corev1.RestartPolicyNever,
			Volumes: []corev1.Volume{
				common.RenderVolumeSlurmConfigs(clusterName),
				common.RenderVolumeMungeKey(secrets.MungeKey.Name, secrets.MungeKey.Key),
				common.RenderVolumeJailFromSource(volumeSources, *ncclBenchmark.VolumeJail.VolumeSourceName),
			},
			Containers: []corev1.Container{renderContainerNCCLBenchmark(ncclBenchmark)},
		},
	}
	cj.Spec.JobTemplate.Spec.Template = pts

	err := common.SetVersions(&cj, &pts)
	if err != nil {
		return batchv1.CronJob{}, err
	}

	return cj, nil
}
