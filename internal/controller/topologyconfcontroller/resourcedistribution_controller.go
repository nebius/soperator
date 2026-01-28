package topologyconfcontroller

import (
	"context"
	"fmt"
	"time"

	kruisev1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controllerconfig"
)

const (
	// TopologyDistributionReconcilerName is the name of this controller
	TopologyDistributionReconcilerName = "TopologyDistributionReconciler"
	// ResourceDistributionName is the name of the ResourceDistribution resource
	ResourceDistributionName = "topology-node-labels-distribution"
)

// +kubebuilder:rbac:groups=apps.kruise.io,resources=resourcedistributions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=nodesets,verbs=get;list;watch

// TopologyDistributionReconciler watches NodeSets and manages ResourceDistribution
// to sync topology-node-labels ConfigMap to namespaces with ephemeral NodeSets.
type TopologyDistributionReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Namespace string
}

// NewTopologyDistributionReconciler creates a new TopologyDistributionReconciler
func NewTopologyDistributionReconciler(
	client client.Client,
	scheme *runtime.Scheme,
	namespace string,
) *TopologyDistributionReconciler {
	return &TopologyDistributionReconciler{
		Client:    client,
		Scheme:    scheme,
		Namespace: namespace,
	}
}

// Reconcile handles NodeSet changes and updates ResourceDistribution accordingly
func (r *TopologyDistributionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(TopologyDistributionReconcilerName)
	logger.Info("Starting reconciliation", "trigger", req.NamespacedName, "namespace", req.Namespace)

	shouldSyncConfigMap, err := r.hasNamespaceWithEphemeralNodeSets(ctx, req.Namespace)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("list ephemeral nodesets: %w", err)
	}

	if !shouldSyncConfigMap {
		logger.Info("No ephemeral NodeSets in namespace, skipping", "namespace", req.Namespace)
		return r.deleteResourceDistributionIfExists(ctx, req.Namespace, logger)
	}

	if err := r.ensureResourceDistribution(ctx, req.Namespace); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure resource distribution: %w", err)
	}

	logger.Info("Successfully ensured ResourceDistribution", "targetNamespaces", req.Namespace)
	return ctrl.Result{}, nil
}

// hasNamespaceWithEphemeralNodeSets checks if there are any NodeSets with EphemeralNodes=true in the given namespace
func (r *TopologyDistributionReconciler) hasNamespaceWithEphemeralNodeSets(ctx context.Context, namespace string) (bool, error) {
	nodeSetList := &slurmv1alpha1.NodeSetList{}
	if err := r.Client.List(ctx, nodeSetList, client.InNamespace(namespace)); err != nil {
		return false, fmt.Errorf("list nodesets: %w", err)
	}

	for _, nodeSet := range nodeSetList.Items {
		if nodeSet.Spec.EphemeralNodes != nil && *nodeSet.Spec.EphemeralNodes {
			return true, nil
		}
	}
	return false, nil
}

// ensureResourceDistribution creates or updates the ResourceDistribution
func (r *TopologyDistributionReconciler) ensureResourceDistribution(
	ctx context.Context,
	targetNamespace string,
) error {
	logger := log.FromContext(ctx).WithName(TopologyDistributionReconcilerName)

	rdName := ResourceDistributionName + "-" + targetNamespace

	targetNamespacesList := []kruisev1alpha1.ResourceDistributionNamespace{{Name: targetNamespace}}

	desired := &kruisev1alpha1.ResourceDistribution{
		ObjectMeta: metav1.ObjectMeta{
			Name: rdName,
		},
		Spec: kruisev1alpha1.ResourceDistributionSpec{
			Resource: runtime.RawExtension{
				Object: &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      consts.ConfigMapNameTopologyNodeLabels,
						Namespace: r.Namespace,
					},
				},
			},
			Targets: kruisev1alpha1.ResourceDistributionTargets{
				IncludedNamespaces: kruisev1alpha1.ResourceDistributionTargetNamespaces{
					List: targetNamespacesList,
				},
			},
		},
	}

	existing := &kruisev1alpha1.ResourceDistribution{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: rdName}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating ResourceDistribution", "name", rdName)
			return r.Client.Create(ctx, desired)
		}
		return fmt.Errorf("get existing resource distribution: %w", err)
	}

	if equalResourceDistributions(existing, desired) {
		logger.Info("ResourceDistribution is up-to-date", "name", rdName)
		return nil
	}

	existing.Spec = desired.Spec
	logger.Info("Updating ResourceDistribution", "name", rdName)
	return r.Client.Update(ctx, existing)
}

// deleteResourceDistributionIfExists removes ResourceDistribution if it exists
func (r *TopologyDistributionReconciler) deleteResourceDistributionIfExists(
	ctx context.Context,
	namespace string,
	logger interface {
		Info(msg string, keysAndValues ...any)
	},
) (ctrl.Result, error) {
	rdName := ResourceDistributionName + "-" + namespace
	rd := &kruisev1alpha1.ResourceDistribution{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: rdName}, rd)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get resource distribution: %w", err)
	}

	logger.Info("No ephemeral NodeSets found, deleting ResourceDistribution", "name", rdName)
	if err := r.Client.Delete(ctx, rd); err != nil {
		return ctrl.Result{}, fmt.Errorf("delete resource distribution: %w", err)
	}
	return ctrl.Result{}, nil
}

// equalResourceDistributions checks if two ResourceDistribution specs are equal
func equalResourceDistributions(
	a, b *kruisev1alpha1.ResourceDistribution,
) bool {
	if len(a.Spec.Targets.IncludedNamespaces.List) != len(b.Spec.Targets.IncludedNamespaces.List) {
		return false
	}
	for i, nsA := range a.Spec.Targets.IncludedNamespaces.List {
		nsB := b.Spec.Targets.IncludedNamespaces.List[i]
		if nsA.Name != nsB.Name {
			return false
		}
	}
	return true
}

// SetupWithManager sets up the controller with the Manager
func (r *TopologyDistributionReconciler) SetupWithManager(
	mgr ctrl.Manager,
	maxConcurrency int,
	cacheSyncTimeout time.Duration,
) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(TopologyDistributionReconcilerName).
		For(&slurmv1alpha1.NodeSet{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				ns, ok := e.Object.(*slurmv1alpha1.NodeSet)
				if !ok {
					return false
				}
				return ns.Spec.EphemeralNodes != nil && *ns.Spec.EphemeralNodes
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldNS, ok := e.ObjectOld.(*slurmv1alpha1.NodeSet)
				if !ok {
					return false
				}
				newNS, ok := e.ObjectNew.(*slurmv1alpha1.NodeSet)
				if !ok {
					return false
				}
				oldEphemeral := oldNS.Spec.EphemeralNodes != nil && *oldNS.Spec.EphemeralNodes
				newEphemeral := newNS.Spec.EphemeralNodes != nil && *newNS.Spec.EphemeralNodes
				return oldEphemeral != newEphemeral
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				ns, ok := e.Object.(*slurmv1alpha1.NodeSet)
				if !ok {
					return false
				}
				return ns.Spec.EphemeralNodes != nil && *ns.Spec.EphemeralNodes
			},
		})).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}
