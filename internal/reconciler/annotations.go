package reconciler

import (
	"fmt"
	"maps"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"nebius.ai/slurm-operator/internal/consts"
)

func GenerateDependencyVersionMap(deps []metav1.Object) map[string]string {
	res := map[string]string{}

	for _, d := range deps {
		res[fmt.Sprintf("%s.%s", d.GetNamespace(), d.GetName())] = d.GetResourceVersion()
	}

	return res
}

func GetVersionsAnnotation(obj metav1.Object) (map[string]string, error) {
	var err error
	res := map[string]string{}

	defer func() {
		if err != nil {
			err = fmt.Errorf("getting versions annotation from object %q: %w", obj.GetName(), err)
		}
	}()

	if obj.GetAnnotations() == nil {
		return res, err
	}

	err = yaml.UnmarshalStrict([]byte(obj.GetAnnotations()[consts.AnnotationVersions]), &res)
	if err != nil {
		err = fmt.Errorf("unmarshalling versions annotation: %w", err)
		return res, err
	}

	return res, err
}

func SetVersionsAnnotation(obj metav1.Object, versions map[string]string) error {
	var err error

	defer func() {
		if err != nil {
			err = fmt.Errorf("setting versions annotation to object %q: %w", obj.GetName(), err)
		}
	}()

	versionsYaml, err := yaml.Marshal(versions)
	if err != nil {
		err = fmt.Errorf("marshaling versions annotation: %w", err)
		return err
	}

	annotations := map[string]string{}
	maps.Copy(annotations, obj.GetAnnotations())
	annotations[consts.AnnotationVersions] = string(versionsYaml)

	obj.SetAnnotations(annotations)

	return err
}

// UpdateVersionsAnnotation given an object ('obj') and its dependencies ('deps'), updates the 'versions' annotation
// with current values of the 'dependencies' 'resourceVersion' fields.
// The annotation 'versions' is a YAML string. Dependency versions are stored there as key-value pairs in the form of
// '{dependency namespace}.{dependency name}: {dependency resourceVersion}'.
// If 'obj' is a 'StatefulSet', the annotation 'versions' is also updated in its 'PodTemplate'.
func UpdateVersionsAnnotation(updated metav1.Object, deps []metav1.Object) error {
	versions, err := GetVersionsAnnotation(updated)
	if err != nil {
		return err
	}

	maps.Copy(versions, GenerateDependencyVersionMap(deps))
	err = SetVersionsAnnotation(updated, versions)
	if err != nil {
		return err
	}

	switch o := updated.(type) {
	case *appsv1.StatefulSet:
		err := UpdateVersionsAnnotation(&o.Spec.Template, deps)
		if err != nil {
			return fmt.Errorf("updating StatefulSet template versions annotation: %w", err)
		}
	}

	return nil
}
