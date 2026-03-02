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
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
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
	scheme = runtime.NewScheme()
	log    = ctrl.Log.WithName("power-manager")
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

	namespace := flag.String("namespace", "", "Kubernetes namespace (auto-detected from ServiceAccount if not specified)")
	nodes := flag.String("nodes", "", "Node list from Slurm (e.g., 'worker-[0-5],gpu-[2-4]') (required)")
	timeout := flag.Duration("timeout", 30*time.Second, "Timeout for operations")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s <command> [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  resume        Resume (power on) nodes - called by Slurm's ResumeProgram\n")
		fmt.Fprintf(os.Stderr, "  suspend       Suspend (power off) nodes - called by Slurm's SuspendProgram\n")
		fmt.Fprintf(os.Stderr, "  wait-added    Wait for nodes to appear in activeNodes - verify resume completed\n")
		fmt.Fprintf(os.Stderr, "  wait-removed  Wait for nodes to be removed from activeNodes - verify suspend completed\n")
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

		if err := updateNodeSetPowerState(ctx, client, namespace, nodeSetName, ordinals, resume); err != nil {
			log.Error(err, "Failed to update NodeSetPowerState", "nodeSet", nodeSetName)
			return err
		}

		log.Info("Updated NodeSetPowerState", "nodeSet", nodeSetName, "action", action, "ordinals", ordinals)
	}

	log.Info("Power action completed successfully", "action", action)
	return nil
}

// waitForNodes waits for specified nodes to appear in or be removed from activeNodes of their NodeSetPowerState CRs.
// If waitForAdded is true, it waits for nodes to appear (used by ResumeProgram).
// If waitForAdded is false, it waits for nodes to be removed (used by SuspendProgram).
func waitForNodes(ctx context.Context, namespace, nodes string, timeout time.Duration, waitForAdded bool) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	action := "appear in"
	if !waitForAdded {
		action = "be removed from"
	}

	log.Info("Waiting for nodes", "action", action, "nodes", nodes, "namespace", namespace, "timeout", timeout)

	client, err := createClient()
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	nodeRefs, err := parseNodeList(nodes)
	if err != nil {
		return fmt.Errorf("failed to parse node list: %w", err)
	}

	nodesByNodeSet := groupNodesByNodeSet(nodeRefs)

	pollInterval := 2 * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for nodes to %s activeNodes", action)
		case <-ticker.C:
			allDone := true
			for nodeSetName, ordinals := range nodesByNodeSet {
				done, err := checkNodesStatus(ctx, client, namespace, nodeSetName, ordinals, waitForAdded)
				if err != nil {
					log.V(1).Info("Error checking activeNodes, will retry", "nodeSet", nodeSetName, "error", err)
					allDone = false
					continue
				}
				if !done {
					log.V(1).Info("Nodes not yet in expected state", "nodeSet", nodeSetName, "ordinals", ordinals, "waitForAdded", waitForAdded)
					allDone = false
				}
			}
			if allDone {
				log.Info("All nodes in expected state", "waitForAdded", waitForAdded)
				return nil
			}
		}
	}
}

// checkNodesStatus checks if all specified ordinals are in or not in the NodeSetPowerState's activeNodes.
// If checkAdded is true, returns true only if ALL ordinals are in activeNodes.
// If checkAdded is false, returns true only if NONE of the ordinals are in activeNodes.
func checkNodesStatus(ctx context.Context, client ctrlclient.Client, namespace, nodeSetName string, ordinals []int32, checkAdded bool) (bool, error) {
	powerState := &slurmv1alpha1.NodeSetPowerState{}
	if err := client.Get(ctx, ctrlclient.ObjectKey{
		Namespace: namespace,
		Name:      nodeSetName,
	}, powerState); err != nil {
		return false, fmt.Errorf("failed to get NodeSetPowerState: %w", err)
	}

	activeSet := make(map[int32]bool, len(powerState.Spec.ActiveNodes))
	for _, ordinal := range powerState.Spec.ActiveNodes {
		activeSet[ordinal] = true
	}

	if checkAdded {
		// All ordinals must be present
		for _, ordinal := range ordinals {
			if !activeSet[ordinal] {
				return false, nil
			}
		}
		return true, nil
	}

	// All ordinals must be absent
	for _, ordinal := range ordinals {
		if activeSet[ordinal] {
			return false, nil
		}
	}
	return true, nil
}

// createClient creates a Kubernetes client.
// It first tries to use in-cluster configuration (when running inside a pod),
// and falls back to kubeconfig (for local development/testing).
func createClient() (ctrlclient.Client, error) {
	config, err := getKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes config: %w", err)
	}

	return ctrlclient.New(config, ctrlclient.Options{Scheme: scheme})
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
// It uses retry.RetryOnConflict to safely handle concurrent resume/suspend calls.
func updateNodeSetPowerState(ctx context.Context, client ctrlclient.Client, namespace, nodeSetName string, ordinals []int32, resume bool) error {
	powerStateKey := ctrlclient.ObjectKey{
		Namespace: namespace,
		Name:      nodeSetName,
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		powerState := &slurmv1alpha1.NodeSetPowerState{}
		err := client.Get(ctx, powerStateKey, powerState)
		if err != nil {
			if ctrlclient.IgnoreNotFound(err) != nil {
				return fmt.Errorf("failed to get NodeSetPowerState: %w", err)
			}
			powerState = &slurmv1alpha1.NodeSetPowerState{}
			powerState.Name = nodeSetName
			powerState.Namespace = namespace
			powerState.Spec.NodeSetRef = nodeSetName
			powerState.Spec.ActiveNodes = []int32{}

			if err := client.Create(ctx, powerState); err != nil {
				return fmt.Errorf("failed to create NodeSetPowerState: %w", err)
			}
			return nil
		}

		currentActiveSet := make(map[int32]bool, len(powerState.Spec.ActiveNodes))
		for _, ord := range powerState.Spec.ActiveNodes {
			currentActiveSet[ord] = true
		}

		if resume {
			for _, ord := range ordinals {
				currentActiveSet[ord] = true
			}
		} else {
			for _, ord := range ordinals {
				delete(currentActiveSet, ord)
			}
		}

		newActiveNodes := make([]int32, 0, len(currentActiveSet))
		for ord := range currentActiveSet {
			newActiveNodes = append(newActiveNodes, ord)
		}
		sort.Slice(newActiveNodes, func(i, j int) bool {
			return newActiveNodes[i] < newActiveNodes[j]
		})

		powerState.Spec.ActiveNodes = newActiveNodes

		return client.Update(ctx, powerState)
	})
}
