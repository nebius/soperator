package artifacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type mk8sCollector struct {
	local     framework.ArgsScope
	projectID string
}

type mk8sClusterList struct {
	Items []struct {
		Metadata struct {
			ID string `json:"id"`
		} `json:"metadata"`
	} `json:"items"`
}

// NewMK8sCollector creates a collector for Managed Kubernetes resources in one project.
func NewMK8sCollector(local framework.ArgsScope, projectID string) Collector {
	return &mk8sCollector{local: local, projectID: strings.TrimSpace(projectID)}
}

func (c *mk8sCollector) Name() string { return "mk8s" }

func (c *mk8sCollector) Collect(ctx context.Context, destination string) error {
	if c.projectID == "" {
		log.Printf("artifacts: skip mk8s collector: Nebius project ID is not provided")
		return nil
	}
	clustersJSON, err := c.local.Run(ctx,
		"nebius", "mk8s", "cluster", "list", "--parent-id", c.projectID, "--format", "json")
	if err != nil {
		return fmt.Errorf("list Managed Kubernetes clusters: %w", err)
	}
	var failures []error
	if err := writeArtifact(destination, "clusters.json", clustersJSON); err != nil {
		failures = append(failures, err)
	}
	if err := collectArgs(ctx, destination, "clusters.txt", c.local,
		"nebius", "mk8s", "cluster", "list", "--parent-id", c.projectID, "--format", "table"); err != nil {
		failures = append(failures, err)
	}

	var clusters mk8sClusterList
	if err := json.Unmarshal([]byte(clustersJSON), &clusters); err != nil {
		failures = append(failures, fmt.Errorf("decode Managed Kubernetes clusters: %w", err))
		return errors.Join(failures...)
	}
	for _, cluster := range clusters.Items {
		clusterID := strings.TrimSpace(cluster.Metadata.ID)
		if clusterID == "" {
			failures = append(failures, fmt.Errorf("Managed Kubernetes cluster has empty ID"))
			continue
		}
		dir := filepath.Join("node-groups", sanitizeCollectorName(clusterID))
		if err := collectArgs(ctx, destination, filepath.Join(dir, "node-groups.json"), c.local,
			"nebius", "mk8s", "node-group", "list", "--parent-id", clusterID, "--page-size", "1000", "--format", "json"); err != nil {
			failures = append(failures, err)
		}
		if err := collectArgs(ctx, destination, filepath.Join(dir, "node-groups.txt"), c.local,
			"nebius", "mk8s", "node-group", "list", "--parent-id", clusterID, "--page-size", "1000", "--format", "table"); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
