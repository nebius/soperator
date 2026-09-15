package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const orphanCleanupPollInterval = 15 * time.Second

type orphanResourceKind struct {
	name          string
	commandPath   []string
	deleteArgs    []string
	prepareDelete func(context.Context, cloudResource) error
}

var orphanResourceKinds = []orphanResourceKind{
	{name: "MK8s cluster", commandPath: []string{"mk8s", "cluster"}},
	{name: "NVLink instance group", commandPath: []string{"compute", "nvl-instance-group"}},
	{name: "GPU cluster", commandPath: []string{"compute", "gpu-cluster"}},
	{name: "filesystem", commandPath: []string{"compute", "filesystem"}},
	{name: "VPC allocation", commandPath: []string{"vpc", "allocation"}},
	{
		name:          "storage bucket",
		commandPath:   []string{"storage", "bucket"},
		deleteArgs:    []string{"--ttl", "0s"},
		prepareDelete: prepareBackupsBucketDelete,
	},
	{name: "service account", commandPath: []string{"iam", "service-account"}},
}

type cloudResource struct {
	Metadata struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"metadata"`
}

type cloudResourceList struct {
	Items []cloudResource `json:"items"`
}

type nebiusCommandRunner func(ctx context.Context, args ...string) ([]byte, error)
type orphanCleanupWaiter func(ctx context.Context) error

func cleanupOrphanedCloudResources(ctx context.Context, projectID string) error {
	return cleanupOrphanedCloudResourcesWith(
		ctx,
		projectID,
		orphanResourceKinds,
		runNebiusCommand,
		waitForOrphanCleanupRetry,
	)
}

func cleanupOrphanedCloudResourcesWith(
	ctx context.Context,
	projectID string,
	kinds []orphanResourceKind,
	run nebiusCommandRunner,
	wait orphanCleanupWaiter,
) error {
	if projectID == "" {
		return fmt.Errorf("validate Nebius project ID: value is empty")
	}

	for _, kind := range kinds {
		if err := cleanupOrphanedResourceKind(ctx, projectID, kind, run, wait); err != nil {
			return err
		}
	}
	return nil
}

func cleanupOrphanedResourceKind(
	ctx context.Context,
	projectID string,
	kind orphanResourceKind,
	run nebiusCommandRunner,
	wait orphanCleanupWaiter,
) error {
	for {
		resources, err := listOrphanedResources(ctx, projectID, kind, run)
		if err != nil {
			return err
		}
		if len(resources) == 0 {
			return nil
		}

		for _, resource := range resources {
			if kind.prepareDelete != nil {
				if err := kind.prepareDelete(ctx, resource); err != nil {
					log.Printf(
						"Prepare orphaned %s %s (%s) for deletion failed, will retry: %v",
						kind.name,
						resource.Metadata.Name,
						resource.Metadata.ID,
						err,
					)
					continue
				}
			}

			log.Printf(
				"Deleting orphaned %s: name=%s id=%s project=%s",
				kind.name,
				resource.Metadata.Name,
				resource.Metadata.ID,
				projectID,
			)
			args := append([]string{}, kind.commandPath...)
			args = append(args,
				"delete",
				"--id", resource.Metadata.ID,
			)
			args = append(args, kind.deleteArgs...)
			args = append(args, "--async", "--no-progress")
			if output, err := run(ctx, args...); err != nil {
				log.Printf(
					"Delete orphaned %s %s (%s) failed, will retry: %v\nOutput: %s",
					kind.name,
					resource.Metadata.Name,
					resource.Metadata.ID,
					err,
					strings.TrimSpace(string(output)),
				)
			}
		}

		if err := wait(ctx); err != nil {
			return fmt.Errorf(
				"wait for orphaned %s cleanup (%s): %w",
				kind.name,
				formatCloudResources(resources),
				err,
			)
		}
	}
}

func listOrphanedResources(
	ctx context.Context,
	projectID string,
	kind orphanResourceKind,
	run nebiusCommandRunner,
) ([]cloudResource, error) {
	args := append([]string{}, kind.commandPath...)
	args = append(args,
		"list",
		"--parent-id", projectID,
		"--all",
		"--format", "json",
	)
	output, err := run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf(
			"list %s resources in project %s: %w\nOutput: %s",
			kind.name,
			projectID,
			err,
			strings.TrimSpace(string(output)),
		)
	}

	var list cloudResourceList
	if err := json.Unmarshal(output, &list); err != nil {
		return nil, fmt.Errorf("decode %s resource list: %w", kind.name, err)
	}

	var resources []cloudResource
	for _, resource := range list.Items {
		if !isE2EResourceName(resource.Metadata.Name) {
			continue
		}
		if resource.Metadata.ID == "" {
			return nil, fmt.Errorf("validate %s resource %q: ID is empty", kind.name, resource.Metadata.Name)
		}
		resources = append(resources, resource)
	}
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].Metadata.Name < resources[j].Metadata.Name
	})
	return resources, nil
}

func isE2EResourceName(name string) bool {
	return name == k8sClusterName || strings.HasPrefix(name, k8sClusterName+"-")
}

func formatCloudResources(resources []cloudResource) string {
	formatted := make([]string, 0, len(resources))
	for _, resource := range resources {
		formatted = append(formatted, fmt.Sprintf("%s=%s", resource.Metadata.Name, resource.Metadata.ID))
	}
	return strings.Join(formatted, ", ")
}

func runNebiusCommand(ctx context.Context, args ...string) ([]byte, error) {
	output, err := exec.CommandContext(ctx, "nebius", args...).CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("run nebius %s: %w", strings.Join(args, " "), err)
	}
	return output, nil
}

func waitForOrphanCleanupRetry(ctx context.Context) error {
	timer := time.NewTimer(orphanCleanupPollInterval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
