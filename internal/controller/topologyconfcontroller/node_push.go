package topologyconfcontroller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmpattern "nebius.ai/slurm-operator/internal/utils/slurm/pattern"
)

// unsettableStates name the power states in which a registration does not survive.
//
// slurmctld drops a node's dynamic topology when it restores state for a node that is powered down,
// so a registration written now would be silently discarded by the next reconfigure. The node is
// left alone and picked up once it is up, which is the same condition Slurm itself checks before
// preserving the value.
var unsettableStates = []api.V0044NodeState{
	api.V0044NodeStatePOWEREDDOWN,
	api.V0044NodeStatePOWERINGDOWN,
}

// pushNodeRegistrations makes the topology slurmctld holds for each node match the rendered config.
//
// Workers register themselves when their pod starts, but that registration is lost whenever a
// reconfigure lands while the node is still powered down, and nothing recomputes it afterwards.
// This is the converging half: it compares what the file says against what slurmctld reports and
// corrects the difference, so a lost or stale registration heals on the next reconcile.
//
// It is deliberately cheap. The desired side comes from the file, the current side from the node
// cache the operator already refreshes, and nodes are grouped by identical registration -- so a
// steady cluster costs no request at all, and a re-render costs one request per changed unit
// rather than one per node.
func (r *WorkerTopologyReconciler) pushNodeRegistrations(
	ctx context.Context, slurmCluster *slurmv1.SlurmCluster, jailedConfig *v1alpha1.JailedConfig,
	rendered, structure string,
) error {
	logger := log.FromContext(ctx)

	if !topologyLoaded(jailedConfig, structure) {
		// Pushing a topology slurmctld has not read yet fails with
		// ESLURM_REQUESTED_TOPO_CONFIG_UNAVAILABLE for every node in it, so the wait is not
		// politeness -- it is the difference between converging and hammering a doomed request.
		logger.V(1).Info("Waiting for the reconfigure to land before pushing registrations",
			"structure", structure)
		return nil
	}

	desired, err := desiredRegistrations(rendered)
	if err != nil {
		return fmt.Errorf("compute desired node registrations: %w", err)
	}
	if len(desired) == 0 {
		return nil
	}

	clusterKey := types.NamespacedName{Namespace: slurmCluster.Namespace, Name: slurmCluster.Name}
	nodeCache, found := r.slurmAPIClients.GetNodeCache(clusterKey)
	if !found {
		logger.V(1).Info("No Slurm node cache yet, skipping the registration push")
		return nil
	}
	slurmClient, found := r.slurmAPIClients.GetClient(clusterKey)
	if !found {
		logger.V(1).Info("No Slurm API client yet, skipping the registration push")
		return nil
	}

	byRegistration := diffRegistrations(desired, nodeCache)
	if len(byRegistration) == 0 {
		return nil
	}

	var pushed []string
	for _, registration := range sortedKeys(byRegistration) {
		nodes := slurmpattern.Merge(byRegistration[registration])
		if err := slurmClient.SetNodeTopology(ctx, nodes, registration); err != nil {
			if errors.Is(err, slurmapi.ErrTopologyUnavailable) {
				// The reconfigure was confirmed but this topology is still missing, which means
				// slurmctld is running older content than the JailedConfig believes. Retrying is
				// the whole remedy, and the node keeps whatever it had.
				logger.Info("Slurm has not loaded the topology yet, will retry",
					"nodes", nodes, "registration", registration)
				return nil
			}
			r.recordNodeTopologyPushFailed(slurmCluster, nodes, registration, err)
			return fmt.Errorf("register %s into %q: %w", nodes, registration, err)
		}
		pushed = append(pushed, fmt.Sprintf("%s=%s", nodes, registration))
	}

	summary := strings.Join(pushed, "; ")
	logger.Info("Registered workers into their topologies", "registrations", summary)
	r.recordNodeTopologyPushed(slurmCluster, summary)
	return nil
}

// nodeLookup is the part of the shared Slurm node cache the diff needs.
type nodeLookup interface {
	GetNode(name string) (slurmapi.Node, bool)
}

// diffRegistrations groups the nodes whose registration disagrees with the config by the value they
// need, so each group becomes one hostlist request.
func diffRegistrations(desired map[string]string, nodeCache nodeLookup) map[string][]string {
	byRegistration := make(map[string][]string)

	for _, name := range sortedKeys(desired) {
		registration := desired[name]

		node, known := nodeCache.GetNode(name)
		if !known {
			// Rendered from the NodeSet replica range, so the config can name a node Slurm has no
			// record of yet.
			continue
		}
		if slices.ContainsFunc(unsettableStates, func(state api.V0044NodeState) bool {
			_, has := node.States[state]
			return has
		}) {
			continue
		}
		if node.Topology == registration {
			continue
		}

		byRegistration[registration] = append(byRegistration[registration], name)
	}

	return byRegistration
}

// topologyLoaded reports whether slurmctld is running the topology structure being rendered.
//
// The annotation holds the structure a confirmed reconfigure was performed for: ensureJailedConfig
// keeps the previous value there while a request is outstanding, so it only reaches the desired
// structure once slurmctld has actually re-read the file.
func topologyLoaded(jailedConfig *v1alpha1.JailedConfig, structure string) bool {
	if jailedConfig == nil {
		return false
	}
	if jailedConfig.Annotations[consts.AnnotationTopologyStructure] != structure {
		return false
	}
	return !hasReconfigureRequest(jailedConfig) || reconfigureConfirmed(jailedConfig)
}

func (r *WorkerTopologyReconciler) recordNodeTopologyPushed(slurmCluster *slurmv1.SlurmCluster, summary string) {
	if r.recorder == nil {
		return
	}
	r.recorder.Eventf(slurmCluster, nil, corev1.EventTypeNormal, reasonNodeTopologyPushed,
		actionRegisterNodeTopology, "Registered workers into their topologies: %s", summary)
}

func (r *WorkerTopologyReconciler) recordNodeTopologyPushFailed(
	slurmCluster *slurmv1.SlurmCluster, nodes, registration string, cause error,
) {
	if r.recorder == nil {
		return
	}
	r.recorder.Eventf(slurmCluster, nil, corev1.EventTypeWarning, reasonNodeTopologyPushFailed,
		actionRegisterNodeTopology, "Failed to register %s into %s: %s", nodes, registration, cause.Error())
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
