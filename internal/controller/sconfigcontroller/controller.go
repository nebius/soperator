/*
Copyright 2024 Nebius B.V.

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
	"io"
	"time"

	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/slurmapi"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const SConfigControllerName = "sconfigcontroller"

type File interface {
	io.Writer
	io.Closer
}
type Store interface {
	Open(name string) (File, error)
}

// SConfigControllerReconciler reconciles a SConfigController object
type ControllerReconciler struct {
	*reconciler.Reconciler
	slurmAPIClient slurmapi.Client

	fileStore   Store
	configNames []string
}

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=configmaps,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the SConfigController object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.2/pkg/reconcile
func (r *ControllerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	configMap := &corev1.ConfigMap{}
	if err := r.Get(ctx, req.NamespacedName, configMap); err != nil {
		logger.V(1).Error(err, "Getting ConfigMap produced an error", "configMap", req.Name)
		// Return an error if the ConfigMap cannot be found or other errors occur
		return ctrl.Result{}, err
	}

	for _, configName := range r.configNames {
		configContent, ok := configMap.Data[configName]
		if !ok {
			continue
		}

		logger.V(1).Info("Found config file in the configMap", "configMap", req.NamespacedName, "configName", configName)

		file, err := r.fileStore.Open(configName)

		if _, err = file.Write([]byte(configContent)); err != nil {
			logger.V(1).Error(err, "Writing config data to the file", "configName", configName)
			return ctrl.Result{}, err
		}

		if err = file.Close(); err != nil {
			logger.V(1).Error(err, "Closing file", "configName", configName)
			return ctrl.Result{}, err
		}
	}

	logger.V(1).Info("Requesting Slurm API to reconfigure workers")

	_, err := r.slurmAPIClient.SlurmV0041GetReconfigureWithResponse(ctx)
	if err != nil {
		logger.V(1).Error(err, "Requesting Slurm API produced an error", "method", "reconfigure")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func NewController(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	slurmAPIClient slurmapi.Client,
) *ControllerReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)

	return &ControllerReconciler{
		Reconciler:     r,
		slurmAPIClient: slurmAPIClient,
	}
}

func (r *ControllerReconciler) SetupWithManager(mgr ctrl.Manager,
	maxConcurrency int, cacheSyncTimeout time.Duration) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				cm, ok := e.Object.(*corev1.ConfigMap)
				if !ok {
					return false
				}

				return r.isValidConfigMap(cm)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				cm, ok := e.ObjectNew.(*corev1.ConfigMap)
				if !ok {
					return false
				}

				return r.isValidConfigMap(cm)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				cm, ok := e.Object.(*corev1.ConfigMap)
				if !ok {
					return false
				}

				return r.isValidConfigMap(cm)
			},
		})).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

func (r *ControllerReconciler) isValidConfigMap(cm *corev1.ConfigMap) bool {
	for _, configName := range r.configNames {
		if _, exists := cm.Data[configName]; exists {
			return true
		}
	}

	return false
}
