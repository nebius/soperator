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

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/flowcontrol"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

const (
	// ServiceAccount file paths
	serviceAccountNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	serviceAccountTokenFile     = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	serviceAccountCAFile        = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	// Kubernetes environment variables
	kubernetesServiceHostEnv = "KUBERNETES_SERVICE_HOST"
	kubernetesServicePortEnv = "KUBERNETES_SERVICE_PORT"

	// Default Kubernetes API server address when running inside a pod
	defaultKubernetesAPIServer = "https://kubernetes.default.svc"
)

var (
	scheme          = runtime.NewScheme()
	log             = ctrl.Log.WithName("power-manager")
	restConfigQPS   = 5.0
	restConfigBurst = 10
	waitForReady    bool
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(slurmv1alpha1.AddToScheme(scheme))
}

// NodeRef represents a parsed node reference with NodeSet name and ordinal
type NodeRef struct {
	NodeSetName string
	Ordinal     int32
}

func main() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(false)))

	flag.Float64Var(&restConfigQPS, "rest-config-qps", 5, "Kubernetes API requests per second per process")
	flag.IntVar(&restConfigBurst, "rest-config-burst", 10, "Kubernetes API request burst per process")
	flag.BoolVar(&waitForReady, "wait", false, "Wait for NodeSet PowerStateReady after applying the action")
	namespace := flag.String("namespace", "", "Kubernetes namespace (auto-detected from ServiceAccount if not specified)")
	nodes := flag.String("nodes", "", "Node list from Slurm (e.g., 'worker-[0-5],gpu-[2-4]') (required)")
	timeout := flag.Duration("timeout", 30*time.Second, "Timeout for operations")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s <command> [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  resume        Resume (power on) nodes - called by Slurm's ResumeProgram\n")
		fmt.Fprintf(os.Stderr, "  suspend       Suspend (power off) nodes - called by Slurm's SuspendProgram\n")
		fmt.Fprintf(os.Stderr, "  wait-added    Wait for ordinals to appear in activeNodes\n")
		fmt.Fprintf(os.Stderr, "  wait-removed  Wait for ordinals to disappear from activeNodes\n")
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s resume --nodes='worker-[0-5],gpu-[2-4]'\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s suspend --nodes='worker-3' --namespace=slurm\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s wait-added --nodes='worker-[0-5]' --timeout=60s\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s wait-removed --nodes='worker-[0-5]' --timeout=60s\n", os.Args[0])
	}

	if len(os.Args) < 2 {
		flag.Usage()
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "resume", "suspend", "wait-added", "wait-removed":
		// ok
	default:
		log.Error(fmt.Errorf("unknown command: %s", command), "Invalid command")
		flag.Usage()
		os.Exit(1)
	}

	if err := flag.CommandLine.Parse(os.Args[2:]); err != nil {
		log.Error(err, "Error parsing flags")
		os.Exit(1)
	}

	if float32(restConfigQPS) <= 0 || math.IsNaN(restConfigQPS) || math.IsInf(restConfigQPS, 0) || restConfigQPS > math.MaxFloat32 || restConfigBurst <= 0 {
		log.Error(fmt.Errorf("rate limits must be positive"), "Invalid rate limits")
		os.Exit(1)
	}
	if *nodes == "" {
		log.Error(fmt.Errorf("--nodes is required"), "Missing required flag")
		os.Exit(1)
	}

	ns := *namespace
	if ns == "" {
		var err error
		ns, err = getNamespaceFromServiceAccount()
		if err != nil {
			log.Error(fmt.Errorf("--namespace is required (could not auto-detect: %v)", err), "Missing required flag")
			os.Exit(1)
		}
		log.Info("Auto-detected namespace from ServiceAccount", "namespace", ns)
	}

	// Run the power action
	switch command {
	case "resume":
		if err := runPowerAction(context.Background(), ns, *nodes, *timeout, true); err != nil {
			log.Error(err, "Power action failed")
			os.Exit(1)
		}
	case "suspend":
		if err := runPowerAction(context.Background(), ns, *nodes, *timeout, false); err != nil {
			log.Error(err, "Power action failed")
			os.Exit(1)
		}
	case "wait-added":
		if err := waitForNodes(context.Background(), ns, *nodes, *timeout, true); err != nil {
			log.Error(err, "Wait for nodes failed")
			os.Exit(1)
		}
	case "wait-removed":
		if err := waitForNodes(context.Background(), ns, *nodes, *timeout, false); err != nil {
			log.Error(err, "Wait for nodes removed failed")
			os.Exit(1)
		}
	}
}

// getNamespaceFromServiceAccount reads the namespace from the ServiceAccount token mount
func getNamespaceFromServiceAccount() (string, error) {
	data, err := os.ReadFile(serviceAccountNamespaceFile)
	if err != nil {
		return "", fmt.Errorf("failed to read namespace file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func runPowerAction(ctx context.Context, namespace, nodes string, timeout time.Duration, resume bool) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	action := "suspend"
	if resume {
		action = "resume"
	}

	log.Info("Starting power action", "action", action, "nodes", nodes, "namespace", namespace)

	client, err := createClient()
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	nodeRefs, err := parseNodeList(nodes)
	if err != nil {
		return fmt.Errorf("failed to parse node list: %w", err)
	}

	log.Info("Parsed nodes", "count", len(nodeRefs))

	nodesByNodeSet := groupNodesByNodeSet(nodeRefs)

	nodeSets := &slurmv1alpha1.NodeSetList{}
	if err := client.List(ctx, nodeSets, ctrlclient.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list NodeSets: %w", err)
	}

	nodeSetMap := make(map[string]*slurmv1alpha1.NodeSet)
	for i := range nodeSets.Items {
		ns := &nodeSets.Items[i]
		nodeSetMap[ns.Name] = ns
	}

	targets := make(map[string]*slurmv1alpha1.NodeSetPowerState)
	for nodeSetName, ordinals := range nodesByNodeSet {
		nodeSet, exists := nodeSetMap[nodeSetName]
		if !exists {
			log.Info("NodeSet not found, skipping", "nodeSet", nodeSetName)
			continue
		}

		if nodeSet.Spec.EphemeralNodes == nil || !*nodeSet.Spec.EphemeralNodes {
			log.Info("NodeSet is not ephemeral, skipping", "nodeSet", nodeSetName)
			continue
		}

		powerState, err := updateNodeSetPowerState(ctx, client, namespace, nodeSetName, ordinals, resume)
		if err != nil {
			log.Error(err, "Failed to update NodeSetPowerState", "nodeSet", nodeSetName)
			return err
		}

		targets[nodeSetName] = powerState
		log.Info("Updated NodeSetPowerState", "nodeSet", nodeSetName, "action", action, "ordinals", ordinals)
	}

	if waitForReady {
		if err := waitForPowerStates(ctx, client, namespace, nodesByNodeSet, targets, resume); err != nil {
			return err
		}
	}
	log.Info("Power action completed successfully", "action", action)
	return nil
}

// waitForNodes waits for future activeNodes membership changes.
// Slurm scripts use --wait on the action itself to retain its original UID and generation.
func waitForNodes(ctx context.Context, namespace, nodes string, timeout time.Duration, waitForAdded bool) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	action := "appear in"
	if !waitForAdded {
		action = "be removed from"
	}

	log.Info("Waiting for applied power state pods", "action", action, "nodes", nodes, "namespace", namespace, "timeout", timeout)

	client, err := createClient()
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	nodeRefs, err := parseNodeList(nodes)
	if err != nil {
		return fmt.Errorf("failed to parse node list: %w", err)
	}

	nodesByNodeSet := groupNodesByNodeSet(nodeRefs)

	group, ctx := errgroup.WithContext(ctx)
	for name, ordinals := range nodesByNodeSet {
		group.Go(func() error {
			isEphemeral, err := nodeSetIsEphemeral(ctx, client, namespace, name)
			if err != nil {
				return err
			}
			if !isEphemeral {
				return nil
			}
			return watchActiveNodes(ctx, client, namespace, name, ordinals, waitForAdded)
		})
	}
	return group.Wait()
}

// nodeSetIsEphemeral reports whether power actions apply to the NodeSet. A NodeSet that is gone
// counts as non-ephemeral: nothing will update its NodeSetPowerState either.
func nodeSetIsEphemeral(ctx context.Context, client ctrlclient.Client, namespace, nodeSetName string) (bool, error) {
	nodeSet := &slurmv1alpha1.NodeSet{}
	key := ctrlclient.ObjectKey{
		Namespace: namespace,
		Name:      nodeSetName,
	}
	backoff := wait.Backoff{Steps: 8, Duration: 100 * time.Millisecond, Factor: 2, Jitter: 0.2, Cap: 5 * time.Second}
	err := backoff.DelayFunc().Until(ctx, true, true, func(context.Context) (bool, error) {
		err := client.Get(ctx, key, nodeSet)
		if isRetryableAPIError(err) {
			return false, nil
		}
		return err == nil, err
	})
	if err != nil {
		if ctrlclient.IgnoreNotFound(err) == nil {
			return false, nil
		}
		return false, fmt.Errorf("get NodeSet: %w", err)
	}

	return nodeSet.Spec.EphemeralNodes != nil && *nodeSet.Spec.EphemeralNodes, nil
}

// createClient creates a Kubernetes client.
// It first tries to use in-cluster configuration (when running inside a pod),
// and falls back to kubeconfig (for local development/testing).
func createClient() (ctrlclient.WithWatch, error) {
	config, err := getKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes config: %w", err)
	}

	setClientRateLimits(config, float32(restConfigQPS), restConfigBurst)
	return ctrlclient.NewWithWatch(config, ctrlclient.Options{Scheme: scheme})
}

func setClientRateLimits(config *rest.Config, qps float32, burst int) {
	config.QPS = qps
	config.Burst = burst
	config.RateLimiter = flowcontrol.NewTokenBucketRateLimiter(qps, burst)
}

// getKubeConfig returns a Kubernetes REST config.
// It tries in-cluster config first, then falls back to kubeconfig file.
func getKubeConfig() (*rest.Config, error) {
	// Try standard in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		log.Info("Using in-cluster configuration")
		return config, nil
	}

	log.V(1).Info("Standard in-cluster config not available", "reason", err.Error())

	// Try to build in-cluster config manually from ServiceAccount files
	// This handles cases where KUBERNETES_SERVICE_HOST/PORT env vars are not set
	// but the ServiceAccount token is still mounted (e.g., when called from slurmctld)
	config, err = buildInClusterConfigFromServiceAccount()
	if err == nil {
		log.Info("Using in-cluster configuration from ServiceAccount files")
		return config, nil
	}

	log.Info("In-cluster config not available, falling back to kubeconfig", "reason", err.Error())

	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		kubeconfigPath = filepath.Join(homeDir, ".kube", "config")
	}

	config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to build config from kubeconfig %s: %w", kubeconfigPath, err)
	}

	log.Info("Using kubeconfig", "path", kubeconfigPath)
	return config, nil
}

// buildInClusterConfigFromServiceAccount builds a REST config from mounted ServiceAccount files.
// This is used when the standard in-cluster config fails (e.g., missing env vars)
// but the ServiceAccount token is still available.
func buildInClusterConfigFromServiceAccount() (*rest.Config, error) {
	if _, err := os.Stat(serviceAccountCAFile); err != nil {
		return nil, fmt.Errorf("CA file not found: %w", err)
	}

	if _, err := os.Stat(serviceAccountTokenFile); err != nil {
		return nil, fmt.Errorf("token file not found: %w", err)
	}

	// Determine the API server address
	host := os.Getenv(kubernetesServiceHostEnv)
	port := os.Getenv(kubernetesServicePortEnv)

	var apiServer string
	if host != "" && port != "" {
		apiServer = "https://" + net.JoinHostPort(host, port)
	} else {
		apiServer = defaultKubernetesAPIServer
	}

	return &rest.Config{
		Host:            apiServer,
		TLSClientConfig: rest.TLSClientConfig{CAFile: serviceAccountCAFile},
		BearerTokenFile: serviceAccountTokenFile,
	}, nil
}

// parseNodeList parses Slurm node list format like "worker-[0-5,7],gpu-[2-4]"
// Returns list of NodeRef with NodeSet name and ordinal
func parseNodeList(nodeList string) ([]NodeRef, error) {
	var result []NodeRef

	parts := splitNodeList(nodeList)

	for _, part := range parts {
		refs, err := parseNodeRange(part)
		if err != nil {
			return nil, fmt.Errorf("failed to parse node range '%s': %w", part, err)
		}
		result = append(result, refs...)
	}

	return result, nil
}

// splitNodeList splits a node list by commas, respecting bracket ranges
func splitNodeList(nodeList string) []string {
	var parts []string
	var current strings.Builder
	bracketDepth := 0

	for _, ch := range nodeList {
		switch ch {
		case '[':
			bracketDepth++
			current.WriteRune(ch)
		case ']':
			bracketDepth--
			current.WriteRune(ch)
		case ',':
			if bracketDepth == 0 {
				if s := strings.TrimSpace(current.String()); s != "" {
					parts = append(parts, s)
				}
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}

	if s := strings.TrimSpace(current.String()); s != "" {
		parts = append(parts, s)
	}

	return parts
}

// parseNodeRange parses a single node range like "worker-[0-5,7]" or "worker-3"
func parseNodeRange(nodeRange string) ([]NodeRef, error) {
	// Pattern: name-[range] or name-number
	bracketPattern := regexp.MustCompile(`^(.+)-\[([^\]]+)\]$`)
	simplePattern := regexp.MustCompile(`^(.+)-(\d+)$`)

	if matches := bracketPattern.FindStringSubmatch(nodeRange); matches != nil {
		nodeSetName := matches[1]
		rangeSpec := matches[2]

		ordinals, err := parseRangeSpec(rangeSpec)
		if err != nil {
			return nil, err
		}

		var refs []NodeRef
		for _, ord := range ordinals {
			refs = append(refs, NodeRef{NodeSetName: nodeSetName, Ordinal: ord})
		}
		return refs, nil
	}

	if matches := simplePattern.FindStringSubmatch(nodeRange); matches != nil {
		nodeSetName := matches[1]
		ordinal, err := strconv.ParseInt(matches[2], 10, 32)
		if err != nil {
			return nil, err
		}
		return []NodeRef{{NodeSetName: nodeSetName, Ordinal: int32(ordinal)}}, nil
	}

	return nil, fmt.Errorf("invalid node range format: %s", nodeRange)
}

// parseRangeSpec parses range specification like "0-5,7,10-12"
func parseRangeSpec(spec string) ([]int32, error) {
	var result []int32

	parts := strings.Split(spec, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid range: %s", part)
			}

			start, err := strconv.ParseInt(rangeParts[0], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid range start: %s", rangeParts[0])
			}

			end, err := strconv.ParseInt(rangeParts[1], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid range end: %s", rangeParts[1])
			}

			for i := start; i <= end; i++ {
				result = append(result, int32(i))
			}
		} else {
			num, err := strconv.ParseInt(part, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid number: %s", part)
			}
			result = append(result, int32(num))
		}
	}

	return result, nil
}

// groupNodesByNodeSet groups node references by their NodeSet name
func groupNodesByNodeSet(refs []NodeRef) map[string][]int32 {
	result := make(map[string][]int32)
	for _, ref := range refs {
		result[ref.NodeSetName] = append(result[ref.NodeSetName], ref.Ordinal)
	}
	return result
}

// updateNodeSetPowerState updates the NodeSetPowerState CR for the given NodeSet.
// Conflicts are retried from fresh state; initial CR creation belongs to the NodeSet controller.
func updateNodeSetPowerState(ctx context.Context, client ctrlclient.Client, namespace, nodeSetName string, ordinals []int32, resume bool) (*slurmv1alpha1.NodeSetPowerState, error) {
	powerStateKey := ctrlclient.ObjectKey{
		Namespace: namespace,
		Name:      nodeSetName,
	}

	var result *slurmv1alpha1.NodeSetPowerState
	missing := false
	attempt := func() error {
		powerState := &slurmv1alpha1.NodeSetPowerState{}
		err := client.Get(ctx, powerStateKey, powerState)
		missing = apierrors.IsNotFound(err)
		if err != nil {
			if ctrlclient.IgnoreNotFound(err) != nil {
				return fmt.Errorf("failed to get NodeSetPowerState: %w", err)
			}
			// Let the NodeSet controller create the CR so initial ordinals and ownership
			// remain consistent, including static-to-ephemeral transitions.
			return err
		}

		currentActiveSet := make(map[int32]bool, len(powerState.Spec.ActiveNodes))
		for _, ord := range powerState.Spec.ActiveNodes {
			currentActiveSet[ord] = true
		}

		changed := false
		if resume {
			for _, ord := range ordinals {
				changed = changed || !currentActiveSet[ord]
				currentActiveSet[ord] = true
			}
		} else {
			for _, ord := range ordinals {
				changed = changed || currentActiveSet[ord]
				delete(currentActiveSet, ord)
			}
		}

		if !changed {
			result = powerState
			return nil
		}
		newActiveNodes := make([]int32, 0, len(currentActiveSet))
		for ord := range currentActiveSet {
			newActiveNodes = append(newActiveNodes, ord)
		}
		sort.Slice(newActiveNodes, func(i, j int) bool {
			return newActiveNodes[i] < newActiveNodes[j]
		})

		powerState.Spec.ActiveNodes = newActiveNodes

		if err := client.Update(ctx, powerState); err != nil {
			return err
		}
		result = powerState
		return nil
	}
	err := retryPowerStateUpdate(ctx, wait.Backoff{Steps: 8, Duration: 100 * time.Millisecond, Factor: 2, Jitter: 0.2, Cap: 5 * time.Second}, attempt)
	if err != nil && ctx.Err() != nil && missing {
		return nil, fmt.Errorf("wait for operator to create NodeSetPowerState %s/%s: %w", namespace, nodeSetName, ctx.Err())
	}
	return result, err
}

func retryPowerStateUpdate(ctx context.Context, backoff wait.Backoff, attempt func() error) error {
	// Steps limits delay growth, not attempts: CR creation can take the whole action budget.
	return backoff.DelayFunc().Until(ctx, true, true, func(context.Context) (bool, error) {
		err := attempt()
		if apierrors.IsConflict(err) || apierrors.IsNotFound(err) || isRetryableAPIError(err) {
			return false, nil
		}
		return err == nil, err
	})
}

func isRetryableAPIError(err error) bool {
	var status apierrors.APIStatus
	var networkError net.Error
	return errors.As(err, &status) && (status.Status().Code >= 500 || status.Status().Code == 429) ||
		errors.As(err, &networkError) && networkError.Timeout()
}
