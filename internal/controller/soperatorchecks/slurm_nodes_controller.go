package soperatorchecks

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/slurmapi"
)

var (
	SlurmNodesControllerName = "soperatorchecks.slurmnodes"

	workerPodNameRegex = regexp.MustCompile(`^worker-\d+$`)
)

type SlurmNodesController struct {
	*reconciler.Reconciler
	slurmAPIClients          *slurmapi.ClientSet
	reconcileTimeout         time.Duration
	enabledNodeReplacement   bool
	disableExtensiveCheck    bool
	apiReader                client.Reader // Direct API reader for pagination
	MaintenanceConditionType corev1.NodeConditionType
}

func NewSlurmNodesController(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	slurmAPIClients *slurmapi.ClientSet,
	reconcileTimeout time.Duration,
	enabledNodeReplacement bool,
	disableExtensiveCheck bool,
	apiReader client.Reader,
	maintenanceConditionType corev1.NodeConditionType,
) *SlurmNodesController {
	r := reconciler.NewReconciler(client, scheme, recorder)

	if maintenanceConditionType == "" {
		maintenanceConditionType = consts.DefaultMaintenanceConditionType
	}

	return &SlurmNodesController{
		Reconciler:               r,
		slurmAPIClients:          slurmAPIClients,
		reconcileTimeout:         reconcileTimeout,
		enabledNodeReplacement:   enabledNodeReplacement,
		disableExtensiveCheck:    disableExtensiveCheck,
		apiReader:                apiReader,
		MaintenanceConditionType: maintenanceConditionType,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *SlurmNodesController) SetupWithManager(mgr ctrl.Manager,
	maxConcurrency int, cacheSyncTimeout time.Duration) error {

	return ctrl.NewControllerManagedBy(mgr).Named(SlurmNodesControllerName).
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

func (c *SlurmNodesController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("SlurmNodesController.reconcile")

	logger.Info("Running slurm nodes controller")

	if err := c.processK8SNodesMaintenance(ctx); err != nil {
		logger.V(1).Error(err, "Process K8S node maintenance produced an error")
		return ctrl.Result{}, err
	}

	degradedNodes, err := c.findDegradedNodes(ctx)
	if err != nil {
		logger.V(1).Error(err, "Find degraded nodes produced an error")
		return ctrl.Result{}, err
	}

	logger.V(1).Info(fmt.Sprintf("found %d degraded nodes", len(degradedNodes)))
	var errs []error
	for slurmClusterName, nodes := range degradedNodes {
		for _, node := range nodes {
			if err := c.processDegradedNode(ctx, slurmClusterName, node); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if err := errors.Join(errs...); err != nil {
		logger.V(1).Error(err, "Process degraded nodes produced an error")
		return ctrl.Result{}, err
	}

	// Set RequeueAfter so SlurmNodesController can perform periodical checks against
	// slurm nodes to find degraded nodes and k8s nodes to find maintenance.
	return ctrl.Result{RequeueAfter: c.reconcileTimeout}, nil
}

// TODO: filter slurmNodes by supported slurm clusters
func (c *SlurmNodesController) findDegradedNodes(ctx context.Context) (map[types.NamespacedName][]slurmapi.Node, error) {
	degradedNodes := make(map[types.NamespacedName][]slurmapi.Node)

	for slurmClusterName, slurmAPIClient := range c.slurmAPIClients.GetClients() {
		slurmNodes, err := slurmAPIClient.ListNodes(ctx)
		if err != nil {
			return nil, err
		}

		for _, node := range slurmNodes {
			if _, ok := node.States[api.V0041NodeStateDRAIN]; !ok {
				// Node is not drained, skipping
				continue
			}

			if node.Reason == nil {
				// Node is drained with no reason, skipping
				continue
			}

			for _, wellKnownReason := range consts.SlurmNodeReasonsList {
				if strings.Contains(node.Reason.Reason, wellKnownReason) {
					// For simplicity, we keep only well known part
					node.Reason.OriginalReason = node.Reason.Reason
					node.Reason.Reason = wellKnownReason

					nodes := degradedNodes[slurmClusterName]
					nodes = append(nodes, node)
					degradedNodes[slurmClusterName] = nodes
					break
				}
			}
		}
	}

	return degradedNodes, nil
}

func (c *SlurmNodesController) processDegradedNode(
	ctx context.Context,
	slurmClusterName types.NamespacedName,
	node slurmapi.Node,
) error {

	k8sNode, err := getK8SNode(ctx, c.Client, node.InstanceID)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			return c.undrainSlurmNode(ctx, slurmClusterName, node.Name)
		}
		return fmt.Errorf("get k8s node: %w", err)
	}

	switch node.Reason.Reason {
	case consts.SlurmNodeReasonKillTaskFailed, consts.SlurmNodeReasonNodeReboot:
		return c.processKillTaskFailed(ctx, k8sNode, slurmClusterName, node)
	case consts.SlurmNodeReasonNodeReplacement:
		return c.processSlurmNodeMaintenance(ctx, k8sNode, slurmClusterName, node.Name)
	case consts.SlurmHardwareReasonHC:
		return c.processSetUnhealthy(ctx, k8sNode, slurmClusterName, node)
	case consts.SlurmNodeReasonHC:
		return c.processHealthCheckFailed(ctx, k8sNode, slurmClusterName, node, node.Reason)
	default:
		log.FromContext(ctx).WithName("SlurmNodesController.processDegradedNode").Info(
			"unknown node reason",
			"nodeName", node.Name,
			"reason", node.Reason.Reason,
			"instanceID", node.InstanceID)
		return nil
	}
}

func (c *SlurmNodesController) processSetUnhealthy(
	ctx context.Context,
	k8sNode *corev1.Node,
	slurmClusterName types.NamespacedName,
	slurmNode slurmapi.Node,
) error {
	logger := log.FromContext(ctx).WithName("SlurmNodesController.processSetUnhealthy")

	if !c.enabledNodeReplacement {
		logger.V(1).Info("Skipping extensive check failed processing, node replacement is disabled")
		return nil
	}

	if slurmNode.Reason != nil && slurmNode.Reason.ChangedAt.Before(k8sNode.CreationTimestamp.Time) {
		logger.V(1).Info("Undraining, slurm node drained before degraded condition changed")
		return c.undrainSlurmNode(ctx, slurmClusterName, slurmNode.Name)
	}

	// If HardwareIssuesSuspected is already set to True, no-op
	var suspected corev1.NodeCondition
	for _, cond := range k8sNode.Status.Conditions {
		if cond.Type == consts.HardwareIssuesSuspected {
			suspected = cond
			break
		}
	}
	if suspected.Status == corev1.ConditionTrue {
		logger.V(1).Info("Skip, hardware issues already suspected")
		return nil
	}

	cond := newNodeCondition(
		consts.HardwareIssuesSuspected,
		corev1.ConditionTrue,
		consts.ReasonGPUHealthCheckFailed,
		consts.MessageConditionType(slurmNode.Comment),
	)

	if err := setK8SNodeCondition(ctx, c.Client, k8sNode.Name, cond); err != nil {
		return fmt.Errorf("set k8s node condition: %w", err)
	}

	return nil
}

func (c *SlurmNodesController) processHealthCheckFailed(
	ctx context.Context,
	k8sNode *corev1.Node,
	slurmClusterName types.NamespacedName,
	slurmNode slurmapi.Node,
	nodeReason *slurmapi.NodeReason,
) error {
	logger := log.FromContext(ctx).WithName("SlurmNodesController.processHealthCheckFailed")

	if !c.enabledNodeReplacement {
		logger.V(1).Info("Skipping health check failed processing, node replacement is disabled")
		return nil
	}

	if c.disableExtensiveCheck {
		logger.V(1).Info("Skipping extensive check flow, setting unhealthy right away")
		return c.processSetUnhealthy(ctx, k8sNode, slurmClusterName, slurmNode)
	}

	if slurmNode.Reason.ChangedAt.Before(k8sNode.CreationTimestamp.Time) {
		logger.V(1).Info("Undraining, slurm node drained before degraded condition changed")
		return c.undrainSlurmNode(ctx, slurmClusterName, slurmNode.Name)
	}

	/**
	Health checks have success and failure reactions.
	When a health check fails, we can already create a reservation using failureReaction.addReservation
	There is no need to reactions.drainSlurmNode then execute logic here to handle DRAINED slurm nodes with [HC] reason

	For backward compatability, we add some logic here to handle already drained slurm nodes with [HC] reason and create a reservation for them then undrain them.
	*/

	// Make sure is drained because of a health check failure.
	_, _, err := parseHealthCheckReason(nodeReason.OriginalReason)
	if err != nil {
		return fmt.Errorf("parse health check reason: %w", err)
	}

	// If hardware issue condition is set, leave the node drained until MK8S deletes it
	var hardwareIssuesCondition corev1.NodeCondition
	for _, cond := range k8sNode.Status.Conditions {
		if cond.Type == consts.HardwareIssuesSuspected {
			hardwareIssuesCondition = cond
			break
		}
	}
	if hardwareIssuesCondition.Status == corev1.ConditionTrue {
		// Node is still hardware degraded, skip
		logger.V(1).Info("Skip, still hardware degraded")
		return nil
	}

	logger.V(1).Info("Creating a slurm reservation for drained node with [HC] reason")

	// Create a maintenance reservation for this slurm node to prevent work from being scheduled on it.
	err = c.createMaintenanceReservationForSlurmNode(ctx, slurmClusterName, slurmNode.Name)
	if err != nil {
		return fmt.Errorf("failed to create maintenance reservaiton for slurm node: %w", err)
	}

	// Undrain node after creating the reservation to allow health checks to run.
	err = c.undrainSlurmNode(ctx, slurmClusterName, slurmNode.Name)
	if err != nil {
		return fmt.Errorf("failed to undrain slurm node after creating a maintenance reservaiton: %w", err)
	}

	return nil
}

const MaintenanceReservationPrefix = "suspicious-node"

func (c *SlurmNodesController) createMaintenanceReservationForSlurmNode(
	ctx context.Context,
	slurmClusterName types.NamespacedName,
	slurmNodeName string,
) error {
	logger := log.FromContext(ctx).WithName("SlurmNodesController.createMaintenanceReservationForSlurmNode").V(1).
		WithValues(
			"slurmNodeName", slurmNodeName,
			"slurmCluster", slurmClusterName,
		)
	logger.Info("create maintenance reservation for slurm node")

	slurmAPIClient, found := c.slurmAPIClients.GetClient(slurmClusterName)
	if !found {
		return fmt.Errorf("slurm cluster %v not found", slurmClusterName)
	}

	err := addReservationForNode(ctx, MaintenanceReservationPrefix, slurmNodeName, slurmAPIClient, logger)
	if err != nil {
		return fmt.Errorf("create reservation: %w", err)
	}

	logger.V(1).Info("slurm node added to a maintenance reservation")
	return nil
}

// https://github.com/kubernetes/apimachinery/blob/release-1.33/pkg/apis/meta/v1/types.go#L1640
const MaxReasonLength = 1024

// https://github.com/kubernetes/apimachinery/blob/release-1.33/pkg/apis/meta/v1/types.go#L1648C29-L1648C44
const MaxMessageLength = 32768

var reasonRegex = regexp.MustCompile(`^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$`)

func parseHealthCheckReason(healthCheckReason string) (consts.ReasonConditionType, consts.MessageConditionType, error) {
	// Split the string into reason and message
	parts := strings.SplitN(healthCheckReason, ": ", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid healthCheckReason format")
	}

	// Extract reason and message
	rawReason := strings.TrimPrefix(parts[0], "[node_problem] ")
	message := parts[1]

	// https://github.com/kubernetes/apimachinery/blob/release-1.33/pkg/apis/meta/v1/types.go#L1642
	reason := toCamelCase(rawReason)

	if len(reason) > MaxReasonLength {
		reason = reason[:MaxReasonLength]
	}
	if !reasonRegex.MatchString(reason) {
		return "", "", fmt.Errorf("reason does not match required format")
	}
	if len(message) > MaxMessageLength {
		message = message[:MaxMessageLength]
	}

	return consts.ReasonConditionType(reason), consts.MessageConditionType(message), nil
}

// toCamelCase converts a string to camelCase format
// Removes invalid characters according to ^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$
// and converts to camelCase
func toCamelCase(input string) string {
	if input == "" {
		return ""
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	if regexp.MustCompile(`^[A-Za-z0-9]+$`).MatchString(input) {
		if len(input) > 0 && unicode.IsLetter(rune(input[0])) {
			return strings.ToLower(string(input[0])) + input[1:]
		}
	}

	words := regexp.MustCompile(`[^A-Za-z0-9]+`).Split(input, -1)

	var result strings.Builder
	firstWord := true

	for _, word := range words {
		if word == "" {
			continue
		}

		cleanWord := cleanWord(word)
		if cleanWord == "" {
			continue
		}

		cleanWord = removeLeadingNumbers(cleanWord)
		if cleanWord == "" {
			continue
		}

		if firstWord {
			result.WriteString(strings.ToLower(cleanWord))
			firstWord = false
		} else {
			if len(cleanWord) > 0 {
				result.WriteString(strings.ToUpper(string(cleanWord[0])) + strings.ToLower(cleanWord[1:]))
			}
		}
	}

	finalResult := result.String()

	if finalResult == "" {
		return ""
	}

	if !unicode.IsLetter(rune(finalResult[0])) {
		return ""
	}

	return finalResult
}

// cleanWord removes characters not allowed (keeps only alphanumeric)
func cleanWord(word string) string {
	var result strings.Builder

	for _, r := range word {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// removeLeadingNumbers removes leading digits from the word
func removeLeadingNumbers(word string) string {
	for i, r := range word {
		if !unicode.IsDigit(r) {
			return word[i:]
		}
	}
	return ""
}

func (c *SlurmNodesController) processKillTaskFailed(
	ctx context.Context,
	k8sNode *corev1.Node,
	slurmClusterName types.NamespacedName,
	slurmNode slurmapi.Node,
) error {
	logger := log.FromContext(ctx).WithName("SlurmNodesController.processKillTaskFailed")

	drainWithCondition := func() error {
		if err := c.drainSlurmNodesWithConditionUpdate(
			ctx,
			slurmNode.InstanceID,
			consts.SlurmNodeReasonNodeReboot,
			newNodeCondition(
				consts.SoperatorChecksK8SNodeDegraded,
				corev1.ConditionTrue,
				consts.ReasonNodeNeedReboot,
				consts.MessageSlurmNodeDegraded,
			),
		); err != nil {
			return fmt.Errorf("drain slurm nodes: %w", err)
		}

		return nil
	}

	var degradedCondition corev1.NodeCondition
	for _, cond := range k8sNode.Status.Conditions {
		if cond.Type == consts.SoperatorChecksK8SNodeDegraded {
			degradedCondition = cond
			break
		}
	}

	if degradedCondition == (corev1.NodeCondition{}) {
		// No degraded condition found
		logger.V(1).Info("draining because no degraded condition found")
		return drainWithCondition()
	}

	if degradedCondition.Status == corev1.ConditionTrue {
		// Node is still rebooting, skip
		logger.V(1).Info("skip, still rebooting")
		return nil
	}

	logger = logger.WithValues(
		"reasonChangedAt", slurmNode.Reason.ChangedAt.String(),
		"conditionTransitionTime", degradedCondition.LastTransitionTime.Time.String(),
	)
	if slurmNode.Reason.ChangedAt.Before(degradedCondition.LastTransitionTime.Time) {
		logger.V(1).Info("undraining, slurm node drained before degraded condition changed")
		return c.undrainSlurmNode(ctx, slurmClusterName, slurmNode.Name)
	}

	logger.V(1).Info("draining, slurm node drained after degraded condition changed")
	return drainWithCondition()
}

func (c *SlurmNodesController) processK8SNodesMaintenance(ctx context.Context) error {
	nextToken := ""

	for {
		listK8SNodesResp, err := listK8SNodesWithReader(ctx, c.apiReader, consts.DefaultLimit, nextToken)
		if err != nil {
			return fmt.Errorf("list k8s nodes: %w", err)
		}

		for _, k8sNode := range listK8SNodesResp.Items {
			drainFn := func() error {
				return c.drainSlurmNodesWithConditionUpdate(
					ctx,
					k8sNode.Name,
					consts.SlurmNodeReasonNodeReplacement,
					newNodeCondition(
						consts.SoperatorChecksK8SNodeMaintenance,
						corev1.ConditionTrue,
						consts.ReasonNodeDraining,
						consts.MessageMaintenanceScheduled,
					),
				)
			}

			if err := c.processMaintenance(ctx, &k8sNode, drainFn, nil); err != nil {
				return fmt.Errorf("process maintenance: %w", err)
			}
		}

		if listK8SNodesResp.Continue == "" {
			break
		}
		nextToken = listK8SNodesResp.Continue
	}

	return nil
}

func (c *SlurmNodesController) processSlurmNodeMaintenance(
	ctx context.Context,
	k8sNode *corev1.Node,
	slurmClusterName types.NamespacedName,
	slurmNodeName string) error {

	undrainFn := func() error {
		return c.undrainSlurmNode(ctx, slurmClusterName, slurmNodeName)
	}

	return c.processMaintenance(ctx, k8sNode, nil, undrainFn)
}

func (c *SlurmNodesController) processMaintenance(
	_ context.Context,
	k8sNode *corev1.Node,
	drainFn, undrainFn func() error,
) error {
	if drainFn == nil {
		drainFn = func() error { return nil }
	}
	if undrainFn == nil {
		undrainFn = func() error { return nil }
	}

	var (
		maintenanceCondition corev1.NodeCondition
	)
	for _, cond := range k8sNode.Status.Conditions {
		if cond.Type == c.MaintenanceConditionType {
			maintenanceCondition = cond
			continue
		}
		if cond.Type == consts.HardwareIssuesSuspected {
			maintenanceCondition = cond
			continue
		}
	}

	if maintenanceCondition == (corev1.NodeCondition{}) || maintenanceCondition.Status == corev1.ConditionFalse {
		return undrainFn()
	}

	return drainFn()
}

func (c *SlurmNodesController) drainSlurmNodesWithConditionUpdate(
	ctx context.Context,
	k8sNodeName string,
	reason string,
	condition corev1.NodeCondition,
) error {

	if err := c.drainSlurmNodes(ctx, k8sNodeName, reason); err != nil {
		return fmt.Errorf("drain slurm nodes: %w", err)
	}

	slurmNodesAreFullyDrained, err := c.slurmNodesFullyDrained(ctx, k8sNodeName)
	if err != nil {
		return fmt.Errorf("check that nodes are fully drained: %w", err)
	}

	if !slurmNodesAreFullyDrained {
		// Wait until all slurm nodes are drained.
		return nil
	}

	if err := setK8SNodeCondition(
		ctx,
		c.Client,
		k8sNodeName,
		condition,
	); err != nil {
		return fmt.Errorf("set k8s node condition: %w", err)
	}

	return nil
}

func (c *SlurmNodesController) drainSlurmNodes(
	ctx context.Context,
	k8sNodeName string,
	reason string,
) error {

	podList := &corev1.PodList{}
	if err := c.List(ctx, podList, client.MatchingFields{"spec.nodeName": k8sNodeName}); err != nil {
		return fmt.Errorf("list pods on node %s: %w", k8sNodeName, err)
	}

	var errs []error
	for _, pod := range podList.Items {
		if workerPodNameRegex.MatchString(pod.Name) {
			slurmClusterName := types.NamespacedName{
				Namespace: pod.Namespace,
				Name:      pod.Labels[consts.LabelInstanceKey],
			}

			err := c.drainSlurmNode(ctx, slurmClusterName, pod.Name, reason)
			if err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

func (c *SlurmNodesController) drainSlurmNode(
	ctx context.Context,
	slurmClusterName types.NamespacedName,
	slurmNodeName, reason string,
) error {
	logger := log.FromContext(ctx).WithName("SlurmNodesController.drainSlurmNode").
		WithValues(
			"slurmNodeName", slurmNodeName,
			"drainReason", reason,
			"slurmCluster", slurmClusterName,
		)
	logger.Info("draining slurm node")

	slurmAPIClient, found := c.slurmAPIClients.GetClient(slurmClusterName)
	if !found {
		return fmt.Errorf("slurm cluster %v not found", slurmClusterName)
	}

	resp, err := slurmAPIClient.SlurmV0041PostNodeWithResponse(ctx, slurmNodeName,
		api.V0041UpdateNodeMsg{
			Reason: ptr.To(string(reason)),
			State:  ptr.To([]api.V0041UpdateNodeMsgState{api.V0041UpdateNodeMsgStateDRAIN}),
		},
	)
	if err != nil {
		return fmt.Errorf("post drain slurm node: %w", err)
	}
	if resp.JSON200.Errors != nil && len(*resp.JSON200.Errors) != 0 {
		return fmt.Errorf("post drain returned errors: %v", *resp.JSON200.Errors)
	}

	logger.V(1).Info("slurm node state is updated to DRAIN")
	return nil
}

func (c *SlurmNodesController) slurmNodesFullyDrained(
	ctx context.Context,
	k8sNodeName string,
) (bool, error) {
	logger := log.FromContext(ctx).WithName("SlurmNodesController.slurmNodesFullyDrained")

	logger.Info("checking that slurm nodes are fully drained")
	podList := &corev1.PodList{}
	if err := c.List(ctx, podList, client.MatchingFields{"spec.nodeName": k8sNodeName}); err != nil {
		return false, fmt.Errorf("list pods on node %s: %w", k8sNodeName, err)
	}

	for _, pod := range podList.Items {
		if workerPodNameRegex.MatchString(pod.Name) {
			logger = logger.WithValues("slurmNode", pod.Name, "instanceKey", pod.Labels[consts.LabelInstanceKey])
			logger.Info("found slurm node")

			slurmClusterName := types.NamespacedName{
				Namespace: pod.Namespace,
				Name:      pod.Labels[consts.LabelInstanceKey],
			}

			node, err := c.getSlurmNode(ctx, slurmClusterName, pod.Name)
			if err != nil {
				return false, err
			}
			_, isCompleting := node.States[api.V0041NodeStateCOMPLETING]
			logger.Info("slurm node", "nodeStates", node.States)
			// When epilog is running, node is in COMPLETING state and both IDLE and DRAIN states are set.
			// Example: State=IDLE+COMPLETING+DRAIN+DYNAMIC_NORM
			// We consider node fully drained when it is in IDLE+DRAIN+DYNAMIC_NORM states.
			if !node.IsIdleDrained() || isCompleting {
				logger.Info("slurm node is not fully drained", "nodeStates", node.States)
				return false, nil
			}
			logger.V(1).Info("slurm node is fully drained", "nodeStates", node.States)
		}
	}

	logger.Info("all slurm nodes are fully drained")
	return true, nil
}

func (c *SlurmNodesController) undrainSlurmNode(
	ctx context.Context,
	slurmClusterName types.NamespacedName,
	slurmNodeName string,
) error {
	logger := log.FromContext(ctx).WithName("SlurmNodesController.undrainSlurmNode").V(1).
		WithValues(
			"slurmNodeName", slurmNodeName,
			"slurmCluster", slurmClusterName,
		)
	logger.Info("undraining slurm node")

	slurmAPIClient, found := c.slurmAPIClients.GetClient(slurmClusterName)
	if !found {
		return fmt.Errorf("slurm cluster %v not found", slurmClusterName)
	}

	resp, err := slurmAPIClient.SlurmV0041PostNodeWithResponse(ctx, slurmNodeName,
		api.V0041UpdateNodeMsg{
			State: ptr.To([]api.V0041UpdateNodeMsgState{api.V0041UpdateNodeMsgStateRESUME}),
		},
	)
	if err != nil {
		return fmt.Errorf("post undrain slurm node: %w", err)
	}
	if resp.JSON200.Errors != nil && len(*resp.JSON200.Errors) != 0 {
		return fmt.Errorf("post undrain returned errors: %v", *resp.JSON200.Errors)
	}

	logger.V(1).Info("slurm node state is updated to RESUME")
	return nil
}

func (c *SlurmNodesController) getSlurmNode(
	ctx context.Context,
	slurmClusterName types.NamespacedName,
	slurmNodeName string,
) (slurmapi.Node, error) {

	slurmAPIClient, found := c.slurmAPIClients.GetClient(slurmClusterName)
	if !found {
		return slurmapi.Node{}, fmt.Errorf("slurm cluster %v not found", slurmClusterName)
	}

	node, err := slurmAPIClient.GetNode(ctx, slurmNodeName)
	if err != nil {
		return slurmapi.Node{}, fmt.Errorf("get node: %w", err)
	}

	return node, nil
}
