package reconciler

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/logfield"
)

func (r Reconciler) updateDependencyVersions(ctx context.Context, resource client.Object, deps ...metav1.Object) error {
	// If any dependency is given, update their versions
	if len(deps) > 0 {
		err := setVersionsRecursive(resource, deps...)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to update dependency versions", logfield.ResourceKV(resource)...)
			return errors.Wrap(err, "updating dependency versions")
		}
	}
	return nil
}

func generateDependencyVersionMap(deps ...metav1.Object) map[string]string {
	res := map[string]string{}

	for _, d := range deps {
		res[fmt.Sprintf("%s.%s", d.GetNamespace(), d.GetName())] = d.GetResourceVersion()
	}

	return res
}

func getVersionsAnnotation(resource metav1.Object) (map[string]string, error) {
	getVersionsAnnotationImpl := func() (map[string]string, error) {
		if resource.GetAnnotations() == nil {
			return map[string]string{}, nil
		}

		if _, found := resource.GetAnnotations()[consts.AnnotationVersions]; !found {
			return map[string]string{}, nil
		}

		res := map[string]string{}
		if err := yaml.UnmarshalStrict([]byte(resource.GetAnnotations()[consts.AnnotationVersions]), &res); err != nil {
			return nil, errors.Wrap(err, "unmarshalling versions annotation")
		}
		return res, nil
	}

	res, err := getVersionsAnnotationImpl()
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("getting versions annotation from resource %q", resource.GetName()))
	}
	return res, err
}

// setVersionsAnnotation given an object `resource` and its dependencies `deps`, sets the 'versions' annotation with
// current values of the dependencies' 'resourceVersion' field.
// The annotation 'versions' is a YAML string. Dependency versions are stored there as key-value pairs in the form of
// '{dependency namespace}.{dependency name}: {dependency resourceVersion}'.
func setVersionsAnnotation(resource metav1.Object, deps ...metav1.Object) error {
	setDependencyVersionsImpl := func() error {
		versions, err := getVersionsAnnotation(resource)
		if err != nil {
			return errors.Wrap(err, "getting existing versions annotation")
		}
		maps.Copy(versions, generateDependencyVersionMap(deps...))
		versionsYaml, err := yaml.Marshal(versions)
		if err != nil {
			return errors.Wrap(err, "marshaling versions annotation")
		}

		annotations := map[string]string{}
		maps.Copy(annotations, resource.GetAnnotations())
		annotations[consts.AnnotationVersions] = strings.TrimSpace(string(versionsYaml))
		resource.SetAnnotations(annotations)

		return nil
	}

	err := setDependencyVersionsImpl()
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("setting versions annotation to resource %q", resource.GetName()))
	}
	return nil
}

// setVersionsRecursive given an object `resource` and its dependencies `deps`, sets the 'versions' annotation with
// current values of the dependencies' 'resourceVersion' field.
// If `resource` is one of [appsv1.StatefulSet], [batchv1.CronJob], or [batchv1.Job],
// the annotation 'versions' is also updated in their [k8s.io/api/core/v1.PodTemplateSpec].
func setVersionsRecursive(resource metav1.Object, deps ...metav1.Object) error {
	setDependencyVersionsRecursiveImpl := func() error {
		err := setVersionsAnnotation(resource, deps...)
		if err != nil {
			return errors.Wrap(err, "setting versions annotation to base resource")
		}

		switch o := resource.(type) {
		case *appsv1.StatefulSet:
			err = setVersionsAnnotation(&o.Spec.Template, deps...)
			if err != nil {
				return errors.Wrap(err, "setting StatefulSet pod template versions annotation")
			}
		case *batchv1.CronJob:
			err = setVersionsAnnotation(&o.Spec.JobTemplate.Spec.Template, deps...)
			if err != nil {
				return errors.Wrap(err, "setting CronJob pod template versions annotation")
			}
		case *batchv1.Job:
			err = setVersionsAnnotation(&o.Spec.Template, deps...)
			if err != nil {
				return errors.Wrap(err, "setting Job pod template versions annotation")
			}
		case *appsv1.Deployment:
			err = setVersionsAnnotation(&o.Spec.Template, deps...)
			if err != nil {
				return errors.Wrap(err, "setting Deployment pod template versions annotation")
			}
		}

		return nil
	}

	if err := setDependencyVersionsRecursiveImpl(); err != nil {
		return errors.Wrap(err, fmt.Sprintf("setting versions annotation to resource %q", resource.GetName()))
	}
	return nil
}
