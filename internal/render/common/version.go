package common

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/mitchellh/hashstructure/v2" // keep
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"nebius.ai/slurm-operator/internal/consts"
)

type hashableStruct interface {
	*appsv1.StatefulSet | *batchv1.CronJob
}

// generateVersionsAnnotations generates version values for consts.AnnotationVersions for
// [k8s.io/api/apps/v1.StatefulSet]/[k8s.io/api/batch/v1.CronJob] and their [k8s.io/api/core/v1.PodTemplateSpec]
func generateVersionsAnnotations[T hashableStruct](s T, pts *corev1.PodTemplateSpec) (sVersion, podVersion []byte, err error) {
	sHash, err := hashstructure.Hash(s, hashstructure.FormatV2, nil)
	if err != nil {
		return nil, nil, err
	}
	ptsHash, err := hashstructure.Hash(pts, hashstructure.FormatV2, nil)
	if err != nil {
		return nil, nil, err
	}

	var sType string
	switch t := reflect.TypeOf(s).Elem().Name(); t {
	case "StatefulSet":
		sType = "sts"
	case "CronJob":
		sType = "cj"
	default:
		return nil, nil, fmt.Errorf("unknown type %q of struct to hash", t)
	}

	sVersion, err = yaml.Marshal(map[string]string{
		"self-" + sType: strconv.FormatUint(sHash, 10),
	})
	if err != nil {
		return nil, nil, err
	}

	podVersion, err = yaml.Marshal(map[string]string{
		"self-pts": strconv.FormatUint(ptsHash, 10),
	})
	if err != nil {
		return nil, nil, err
	}

	return sVersion, podVersion, nil
}

// SetVersions sets version value for consts.AnnotationVersions annotation for
// [k8s.io/api/apps/v1.StatefulSet]/[k8s.io/api/batch/v1.CronJob] and their [k8s.io/api/core/v1.PodTemplateSpec]
func SetVersions[T hashableStruct](
	s T,
	pts *corev1.PodTemplateSpec,
) error {
	t := reflect.TypeOf(s).Elem().Name()
	switch t {
	case "StatefulSet":
	case "CronJob":
		_ = true
	default:
		return fmt.Errorf("unknown type %q of struct to set versions annotation", t)
	}

	sVersionBytes, ptsVersionBytes, err := generateVersionsAnnotations(s, pts)
	if err != nil {
		return fmt.Errorf("generating versions annotations: %w", err)
	}
	sVersion, ptsVersion := strings.TrimSpace(string(sVersionBytes)), strings.TrimSpace(string(ptsVersionBytes))

	ensureVersions := func(m map[string]string, v string) map[string]string {
		if m == nil {
			m = map[string]string{}
		}
		m[consts.AnnotationVersions] = v
		return m
	}

	if t == "StatefulSet" {
		ss := any(s).(*appsv1.StatefulSet)
		ss.ObjectMeta.Annotations = ensureVersions(ss.ObjectMeta.Annotations, sVersion)
		ss.Spec.Template.ObjectMeta.Annotations = ensureVersions(ss.Spec.Template.ObjectMeta.Annotations, ptsVersion)
	} else if t == "CronJob" {
		ss := any(s).(*batchv1.CronJob)
		ss.ObjectMeta.Annotations = ensureVersions(ss.ObjectMeta.Annotations, sVersion)
		ss.Spec.JobTemplate.Spec.Template.ObjectMeta.Annotations = ensureVersions(ss.Spec.JobTemplate.Spec.Template.ObjectMeta.Annotations, ptsVersion)
	}

	return nil
}
