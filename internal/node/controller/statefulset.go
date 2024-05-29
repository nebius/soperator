package controller

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/models/k8s"
	"nebius.ai/slurm-operator/internal/models/slurm"
)

// RenderStatefulSet renders new [appsv1.StatefulSet] containing Slurm controller pods
func RenderStatefulSet(cluster smodels.ClusterValues) (appsv1.StatefulSet, error) {
	labels := k8smodels.BuildClusterDefaultLabels(cluster.Name, consts.ComponentTypeController)
	matchLabels := k8smodels.BuildClusterDefaultMatchLabels(cluster.Name, consts.ComponentTypeController)

	stsVersion, podVersion, err := generateVersionsAnnotationPlaceholders()
	if err != nil {
		return appsv1.StatefulSet{}, fmt.Errorf("generating versions annotation placeholders: %w", err)
	}

	return appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Controller.StatefulSet.Name,
			Namespace: cluster.Controller.StatefulSet.Namespace,
			Labels:    labels,
			Annotations: map[string]string{
				consts.AnnotationVersions: string(stsVersion),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: cluster.Controller.Service.Name,
			Replicas:    &cluster.Controller.StatefulSet.Replicas,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					MaxUnavailable: &cluster.Controller.StatefulSet.MaxUnavailable,
				},
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						consts.AnnotationVersions: string(podVersion),
					},
				},
				Spec: corev1.PodSpec{
					Affinity:    cluster.Controller.Affinity,
					Tolerations: cluster.Controller.Tolerations,
					Containers:  []corev1.Container{renderSlurmCtldContainer(cluster)},
					Volumes: []corev1.Volume{
						renderVolumeSlurmKey(),
						renderVolumeSlurmConfigs(),
						renderVolumeSpool(),
					},
				},
			},
		},
	}, nil
}
