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

package sconfigcontroller

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/slurmapi"
)

const (
	configMapField = ".spec.configMap"
)

// JailedConfigReconciler reconciles a JailedConfig object
type JailedConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// TODO add ctor
	slurmAPIClient slurmapi.Client
	fileStore      Store
}

// TODO add configmap access
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs/finalizers,verbs=update

// mostly copy-pasted from k8s ConfigMap volumes
// See https://github.com/kubernetes/kubernetes/blob/b266ac2c3e42c2c4843f81e20213d2b2f43e450a/pkg/volume/configmap/configmap.go/

type JailedFile struct {
	Data []byte
	Mode int32
}

func makePayload(mappings []corev1.KeyToPath, configMap *corev1.ConfigMap, defaultMode *int32) (map[string]JailedFile, error) {
	if defaultMode == nil {
		return nil, fmt.Errorf("no defaultMode used, not even the default value for it")
	}

	payload := make(map[string]JailedFile, len(configMap.Data)+len(configMap.BinaryData))
	var jailedFile JailedFile

	if len(mappings) == 0 {
		for name, data := range configMap.Data {
			jailedFile.Data = []byte(data)
			jailedFile.Mode = *defaultMode
			payload[name] = jailedFile
		}
		for name, data := range configMap.BinaryData {
			jailedFile.Data = data
			jailedFile.Mode = *defaultMode
			payload[name] = jailedFile
		}
	} else {
		for _, ktp := range mappings {
			if stringData, ok := configMap.Data[ktp.Key]; ok {
				jailedFile.Data = []byte(stringData)
			} else if binaryData, ok := configMap.BinaryData[ktp.Key]; ok {
				jailedFile.Data = binaryData
			} else {
				return nil, fmt.Errorf("configmap references non-existent config key: %s", ktp.Key)
			}

			if ktp.Mode != nil {
				jailedFile.Mode = *ktp.Mode
			} else {
				jailedFile.Mode = *defaultMode
			}
			payload[ktp.Path] = jailedFile
		}
	}

	return payload, nil
}

func validatePayloadPath(path string) error {
	switch {
	case !strings.HasPrefix(path, "/"):
		return fmt.Errorf("invalid path %q: must be absolute", path)
	default:
		return nil
	}
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the JailedConfig object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *JailedConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// TODo add more fields
	logger := logf.FromContext(ctx)

	logger.V(1).Info("Reconciling JailedConfig", "req", req)

	jailedConfig := &slurmv1alpha1.JailedConfig{}
	err := r.Client.Get(ctx, req.NamespacedName, jailedConfig)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// JailedConfig not found, so it must have been deleted
			// There's basically just 3 ways to react to deletion:
			// * Do nothing: materialized files will remain in jail FS; simplest option
			// * Unlink file from FS
			// * Truncate/replace file with empty one
			// Replacing files with tombstones is not universal: tombstone can't be part of resource,
			// and hardcoding tombstone here is not flexible for arbitrary file format
			// Unlinking and truncating can lead to issues if between deleting resource and deleting file user will change it, but that's expected0

			logger.V(1).Info("JailedConfig resource not found. Ignoring since object must have been be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{Requeue: true}, fmt.Errorf("getting JailedConfig: %w", err)
	}

	configMap := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, types.NamespacedName{
		Name:      jailedConfig.Spec.ConfigMap.Name,
		Namespace: jailedConfig.Spec.ConfigMap.Name,
	}, configMap)
	if err != nil {
		// Error reading the object - requeue the request.
		return ctrl.Result{Requeue: true}, fmt.Errorf("getting ConfigMap: %w", err)
	}

	if len(jailedConfig.Spec.Items) == 0 {
		return ctrl.Result{}, nil
	}

	jailPayload, err := makePayload(jailedConfig.Spec.Items, configMap, jailedConfig.Spec.DefaultMode)
	if err != nil {
		// Error preparing payload - requeue the request.
		return ctrl.Result{Requeue: true}, fmt.Errorf("getting JailedConfig payload: %w", err)
	}

	for path, payload := range jailPayload {
		if err := validatePayloadPath(path); err != nil {
			return ctrl.Result{}, fmt.Errorf("invalid path %q in ConfigMap annotations: %w", path, err)
		}

		err := r.fileStore.Write(path, payload.Data)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("writing file %q to fileStore: %w", path, err)
		}

		err = r.fileStore.Chmod(path, uint32(payload.Mode))
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("writing file %q to fileStore: %w", path, err)
		}
	}

	// TODO fsync, should not be strictly necessary

	// TODO throttle reconfiguring calls

	// TODO sync on configuration update over whole cluster
	// 	/nodes + .slurmd_start_time

	// https://github.com/SchedMD/slurm/blob/dff6513dc96ae422dda876b22e64ee9149c418ec/src/slurmctld/node_mgr.c#L4539-L4551

	for _, action := range jailedConfig.Spec.UpdateActions {
		switch action {
		case slurmv1alpha1.Reconfigure:
			logger.V(1).Info("Requesting Slurm API to reconfigure workers")
			_, err := r.slurmAPIClient.SlurmV0041GetReconfigureWithResponse(ctx)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("requesting Slurm API to reconfigure workers: %w", err)
			}
		default:
			return ctrl.Result{}, fmt.Errorf("unexcpected update action %s: %w", action, err)
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *JailedConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &slurmv1alpha1.JailedConfig{}, configMapField, func(rawObj client.Object) []string {
		jailedConfig := rawObj.(*slurmv1alpha1.JailedConfig)
		if jailedConfig.Spec.ConfigMap == nil {
			return nil
		}
		if jailedConfig.Spec.ConfigMap.Name == "" {
			return nil
		}
		return []string{jailedConfig.Spec.ConfigMap.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&slurmv1alpha1.JailedConfig{}).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findObjectsForConfigMap),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Named("jailedconfig").
		Complete(r)
}

func (r *JailedConfigReconciler) findObjectsForConfigMap(ctx context.Context, configMapObject client.Object) []reconcile.Request {
	jailedConfigs := &slurmv1alpha1.JailedConfigList{}
	listOps := &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(configMapField, configMapObject.GetName()),
		Namespace:     configMapObject.GetNamespace(),
	}
	err := r.List(ctx, jailedConfigs, listOps)
	if err != nil {
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, len(jailedConfigs.Items))
	for i, item := range jailedConfigs.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
			},
		}
	}
	return requests
}
