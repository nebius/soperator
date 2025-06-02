package soperatorchecks

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/jwt"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/slurmapi"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

var (
	SlurmAPIClientsControllerName = "soperatorchecks.slurmapiclients"
)

type SlurmAPIClientsController struct {
	*reconciler.Reconciler

	slurmAPIClients *slurmapi.ClientSet
}

func NewSlurmAPIClientsController(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	slurmAPIClients *slurmapi.ClientSet,
) *SlurmAPIClientsController {
	r := reconciler.NewReconciler(client, scheme, recorder)

	return &SlurmAPIClientsController{
		Reconciler:      r,
		slurmAPIClients: slurmAPIClients,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *SlurmAPIClientsController) SetupWithManager(mgr ctrl.Manager,
	maxConcurrency int, cacheSyncTimeout time.Duration) error {

	return ctrl.NewControllerManagedBy(mgr).Named(SlurmAPIClientsControllerName).
		For(&slurmv1.SlurmCluster{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return true
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				return false
			},
		})).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

func (c *SlurmAPIClientsController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("SlurmAPIClientsController.reconcile")

	logger.Info("Adding new slurm api client")

	jwtToken := jwt.NewToken(c.Client).For(req.NamespacedName, "root").WithRegistry(jwt.NewTokenRegistry().Build())
	slurmAPIServer := fmt.Sprintf("http://%s.%s:6820", naming.BuildServiceName(consts.ComponentTypeREST, req.Name), req.Namespace)
	slurmAPIClient, err := slurmapi.NewClient(slurmAPIServer, jwtToken, slurmapi.DefaultHTTPClient())
	if err != nil {
		logger.Error(err, "failed to create slurm api client")
		return ctrl.Result{}, err
	}

	c.slurmAPIClients.AddClient(req.NamespacedName, slurmAPIClient)

	return ctrl.Result{}, nil
}
