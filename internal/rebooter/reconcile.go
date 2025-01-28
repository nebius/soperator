package rebooter

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
)

var (
	ControllerName  = "rebooter"
	SlurmNodeDrain  = corev1.NodeConditionType("SlurmNodeDrain")
	SlurmNodeReboot = corev1.NodeConditionType("SlurmNodeReboot")
)

type RebooterReconciler struct {
	*reconciler.Reconciler
	reconcileTimeout time.Duration
}

func NewRebooterReconciler(client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder, reconcileTimeout time.Duration) *RebooterReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)
	return &RebooterReconciler{
		Reconciler:       r,
		reconcileTimeout: reconcileTimeout,
	}
}

func (r *RebooterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(ControllerName)
	logger.Info(fmt.Sprintf("Reconciling %s", req.Name))

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RebooterReconciler) SetupWithManager(
	mgr ctrl.Manager, maxConcurrency int, cacheSyncTimeout time.Duration, nodeName string,
) error {
	ctx := context.Background()

	// Index pods by node name. This is used to list and evict pods from a specific node.
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, "spec.nodeName", func(rawObj client.Object) []string {
		pod := rawObj.(*corev1.Pod)
		// Check if the pod.Spec.NodeName is the same as the nodeName.
		// If it is, return the nodeName to index the pod by it.
		if pod.Spec.NodeName == nodeName {
			return []string{pod.Spec.NodeName}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to setup field indexer: %w", err)
	}

	// Index the nodes by name
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Node{}, "metadata.name", func(rawObj client.Object) []string {
		node := rawObj.(*corev1.Node)
		if node.Name == nodeName {
			return []string{node.Name}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to setup node field indexer: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}, builder.WithPredicates(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				// Rebooter is daemonset and should only reconcile the node it is running on
				// so we compare the node name to the NODE_NAME environment variable
				if e.ObjectNew.GetName() != nodeName {
					return false
				}
				oldNode := e.ObjectOld.(*corev1.Node)
				newNode := e.ObjectNew.(*corev1.Node)

				// Extract the desired conditions from both old and new nodes
				// and compare them to determine if reconciliation is needed
				// based on the conditions changing
				var oldDrainCondition, newDrainCondition, oldRebootCondition, newRebootCondition *corev1.NodeCondition
				for i := range oldNode.Status.Conditions {
					if oldNode.Status.Conditions[i].Type == SlurmNodeDrain {
						oldDrainCondition = &oldNode.Status.Conditions[i]
					} else if oldNode.Status.Conditions[i].Type == SlurmNodeReboot {
						oldRebootCondition = &oldNode.Status.Conditions[i]
					}
				}
				for i := range newNode.Status.Conditions {
					if newNode.Status.Conditions[i].Type == SlurmNodeDrain {
						newDrainCondition = &newNode.Status.Conditions[i]
					} else if newNode.Status.Conditions[i].Type == SlurmNodeReboot {
						newRebootCondition = &newNode.Status.Conditions[i]
					}
				}

				// Trigger reconciliation if the Drain condition has changed
				if oldDrainCondition == nil || newDrainCondition == nil || oldDrainCondition.Status != newDrainCondition.Status {
					return true
				}

				// Trigger reconciliation if the Reboot condition has changed
				if oldRebootCondition == nil || newRebootCondition == nil || oldRebootCondition.Status != newRebootCondition.Status {
					return true
				}

				return false
			},
			CreateFunc: func(e event.CreateEvent) bool {
				return e.Object.GetName() == nodeName
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				return e.Object.GetName() == nodeName
			},
		})).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}
