package common_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/exp/maps"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
)

func TestSetVersions(t *testing.T) {
	t.Run("Test empty StatefulSet", func(t *testing.T) {
		s := appsv1.StatefulSet{}
		pts := corev1.PodTemplateSpec{}
		s.Spec.Template = pts

		err := common.SetVersions(&s, &pts)
		assert.NoError(t, err)

		assert.True(t, slices.Contains(maps.Keys(s.ObjectMeta.Annotations), consts.AnnotationVersions))
		assert.Equal(t, "self-sts: \"13710071407556780422\"", s.ObjectMeta.Annotations[consts.AnnotationVersions])

		assert.True(t, slices.Contains(maps.Keys(s.Spec.Template.ObjectMeta.Annotations), consts.AnnotationVersions))
		assert.Equal(t, "self-pts: \"7555675507055441312\"", s.Spec.Template.ObjectMeta.Annotations[consts.AnnotationVersions])
	})

	t.Run("Test empty CronJob", func(t *testing.T) {
		s := batchv1.CronJob{}
		pts := corev1.PodTemplateSpec{}
		s.Spec.JobTemplate.Spec.Template = pts

		err := common.SetVersions(&s, &pts)
		assert.NoError(t, err)

		assert.True(t, slices.Contains(maps.Keys(s.ObjectMeta.Annotations), consts.AnnotationVersions))
		assert.Equal(t, "self-cj: \"9780505695803152523\"", s.ObjectMeta.Annotations[consts.AnnotationVersions])

		assert.True(t, slices.Contains(maps.Keys(s.Spec.JobTemplate.Spec.Template.ObjectMeta.Annotations), consts.AnnotationVersions))
		assert.Equal(t, "self-pts: \"7555675507055441312\"", s.Spec.JobTemplate.Spec.Template.ObjectMeta.Annotations[consts.AnnotationVersions])
	})

	t.Run("Test StatefulSet with existing annotations", func(t *testing.T) {
		s := appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"a": "b",
				},
			},
		}
		pts := corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"c": "d",
				},
			},
		}
		s.Spec.Template = pts

		err := common.SetVersions(&s, &pts)
		assert.NoError(t, err)

		assert.True(t, slices.Contains(maps.Keys(s.ObjectMeta.Annotations), consts.AnnotationVersions))
		assert.True(t, len(maps.Keys(s.ObjectMeta.Annotations)) > 0)
		assert.Equal(t, "b", s.ObjectMeta.Annotations["a"])
		assert.Equal(t, "self-sts: \"11134765068337800463\"", s.ObjectMeta.Annotations[consts.AnnotationVersions])

		assert.True(t, slices.Contains(maps.Keys(s.Spec.Template.ObjectMeta.Annotations), consts.AnnotationVersions))
		assert.True(t, len(maps.Keys(s.Spec.Template.ObjectMeta.Annotations)) > 0)
		assert.Equal(t, "d", s.Spec.Template.ObjectMeta.Annotations["c"])
		assert.Equal(t, "self-pts: \"16512879597073434496\"", s.Spec.Template.ObjectMeta.Annotations[consts.AnnotationVersions])
	})

	t.Run("Test CronJob with existing annotations", func(t *testing.T) {
		s := batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"a": "b",
				},
			},
		}
		pts := corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"c": "d",
				},
			},
		}
		s.Spec.JobTemplate.Spec.Template = pts

		err := common.SetVersions(&s, &pts)
		assert.NoError(t, err)

		assert.True(t, slices.Contains(maps.Keys(s.ObjectMeta.Annotations), consts.AnnotationVersions))
		assert.True(t, len(maps.Keys(s.ObjectMeta.Annotations)) > 0)
		assert.Equal(t, "b", s.ObjectMeta.Annotations["a"])
		assert.Equal(t, "self-cj: \"4875880741397189077\"", s.ObjectMeta.Annotations[consts.AnnotationVersions])

		assert.True(t, slices.Contains(maps.Keys(s.Spec.JobTemplate.Spec.Template.ObjectMeta.Annotations), consts.AnnotationVersions))
		assert.True(t, len(maps.Keys(s.Spec.JobTemplate.Spec.Template.ObjectMeta.Annotations)) > 0)
		assert.Equal(t, "d", s.Spec.JobTemplate.Spec.Template.ObjectMeta.Annotations["c"])
		assert.Equal(t, "self-pts: \"16512879597073434496\"", s.Spec.JobTemplate.Spec.Template.ObjectMeta.Annotations[consts.AnnotationVersions])
	})
}
