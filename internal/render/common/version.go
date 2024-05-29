package common

import (
	"sigs.k8s.io/yaml"
)

// GenerateVersionsAnnotationPlaceholders generates placeholder values for consts.AnnotationVersions for
// [k8s.io/api/apps/v1.StatefulSet] and its [k8s.io/api/core/v1.PodTemplateSpec]
func GenerateVersionsAnnotationPlaceholders() (stsVersion, podVersion []byte, err error) {
	stsVersion, err = yaml.Marshal(map[string]string{
		"self-sts": "version-placeholder-000",
	})
	if err != nil {
		return nil, nil, err
	}

	podVersion, err = yaml.Marshal(map[string]string{
		"self-pod-tmpl": "version-placeholder-001",
	})
	if err != nil {
		return nil, nil, err
	}

	return stsVersion, podVersion, nil
}
