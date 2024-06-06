package reconciler

import (
	"context"
	"maps"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Reconciler is the base type used for reconciler objects
type Reconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func logResourceKV(obj client.Object) []interface{} {
	return []interface{}{
		"Namespace", obj.GetNamespace(),
		"ResourceName", obj.GetName(),
	}
}

func (r Reconciler) GetNamespacedObject(ctx context.Context, namespace, name string, obj client.Object) error {
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, obj); err != nil {
		return err
	}
	return nil
}

// EnsureDeployed ensures that 'resourceToDeploy' is deployed. If a corresponding resource 'existingResource' is found,
// it doesn't take any action
func (r Reconciler) EnsureDeployed(
	ctx context.Context,
	resourceToDeploy,
	existingResource,
	resourceOwner client.Object,
	deps ...metav1.Object) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	err := r.GetNamespacedObject(ctx, resourceToDeploy.GetNamespace(), resourceToDeploy.GetName(), existingResource)
	if err == nil {
		return ctrl.Result{}, nil
	}

	if !apierrors.IsNotFound(err) {
		logger.Error(err, "Failed to get resource", logResourceKV(resourceToDeploy)...)
		return ctrl.Result{}, err
	}

	if err = ctrl.SetControllerReference(resourceOwner, resourceToDeploy, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Creating new resource", logResourceKV(resourceToDeploy)...)

	// If any dependency is given, set dependencies version
	if len(deps) > 0 {
		err = updateVersionsAnnotation(resourceToDeploy, deps)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	if err = r.Create(ctx, resourceToDeploy); err != nil {
		logger.Error(err, "Failed creating new resource", logResourceKV(resourceToDeploy)...)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// EnsureUpdated ensures that existing resource 'existing' is up-to-date with its last revision 'updated' and its dependencies 'deps'.
// In order to determine if the resource needs to be updated, the 'versions' annotations of 'existing' and 'updated' resources are compared.
// The 'versions' annotation is a YAML string containing key-value pairs in the form of '{resource namespace}.{resource name}: {resource version}'.
// When resource is created, its version can be put into 'versions' in order to trigger its update when the version changes.
// Dependencies' 'deps' versions ('resourceVersion' field) are automatically updated.
// Therefore, if the dependency changes its version, the update is triggered.
// If the resource is a 'StatefulSet', dependency 'versions' annotation is propagated to the relative PodTemplate,
// thus triggering a rolling update of the pods.
func (r Reconciler) EnsureUpdated(
	ctx context.Context,
	updated,
	existing,
	owner client.Object,
	deps ...metav1.Object,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	err := r.GetNamespacedObject(ctx, updated.GetNamespace(), updated.GetName(), existing)
	if err != nil {
		logger.Error(err, "Failed to get resource", logResourceKV(updated)...)
		return ctrl.Result{}, err
	}

	err = updateVersionsAnnotation(updated, deps)
	if err != nil {
		return ctrl.Result{}, err
	}

	existingAnnotations, err := getVersionsAnnotation(existing)
	if err != nil {
		logger.Error(err, "Failed to get existing resource versions annotation", logResourceKV(existing)...)
		return ctrl.Result{}, err
	}
	updatedAnnotations, err := getVersionsAnnotation(updated)
	if err != nil {
		logger.Error(err, "Failed to get updated resource versions annotation", logResourceKV(updated)...)
		return ctrl.Result{}, err
	}

	if !maps.Equal(updatedAnnotations, existingAnnotations) {
		logger.Info("Updating resource", logResourceKV(updated)...)
		if err := ctrl.SetControllerReference(owner, updated, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.Update(ctx, updated); err != nil {
			logger.Error(err, "Failed updating resource", logResourceKV(updated)...)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}
