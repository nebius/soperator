package sharedsteps

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func nodeSetForWorker(
	ctx context.Context,
	kubectl *framework.KubectlClient,
	clusterName string,
	worker framework.WorkerInfo,
) (framework.NodeSetInfo, error) {
	if strings.TrimSpace(worker.NodeSetName) == "" {
		return framework.NodeSetInfo{}, fmt.Errorf("resolve NodeSet for worker %s: NodeSet name is empty", worker.Name)
	}

	nodeSets, err := kubectl.NodeSets(ctx, clusterName)
	if err != nil {
		return framework.NodeSetInfo{}, err
	}
	for _, nodeSet := range nodeSets {
		if nodeSet.Name == worker.NodeSetName {
			return nodeSet, nil
		}
	}
	return framework.NodeSetInfo{}, fmt.Errorf("resolve NodeSet %s for worker %s: NodeSet was not found", worker.NodeSetName, worker.Name)
}

func writableJailSubMountContaining(target string, mounts []framework.JailSubMountInfo) (framework.JailSubMountInfo, error) {
	target, err := cleanAbsolutePath(target)
	if err != nil {
		return framework.JailSubMountInfo{}, err
	}

	var matches []framework.JailSubMountInfo
	for _, mount := range mounts {
		mountPath, err := cleanAbsolutePath(mount.MountPath)
		if err != nil || !pathIsUnder(target, mountPath) {
			continue
		}
		mount.MountPath = mountPath
		matches = append(matches, mount)
	}
	if len(matches) == 0 {
		return framework.JailSubMountInfo{}, fmt.Errorf(
			"find writable jail submount containing %s: configured mounts: %s",
			target,
			describeJailSubMounts(mounts),
		)
	}

	sort.Slice(matches, func(i, j int) bool {
		return len(matches[i].MountPath) > len(matches[j].MountPath)
	})
	if len(matches) > 1 && matches[0].MountPath == matches[1].MountPath {
		return framework.JailSubMountInfo{}, fmt.Errorf(
			"find writable jail submount containing %s: multiple mounts use path %s",
			target,
			matches[0].MountPath,
		)
	}
	if matches[0].ReadOnly {
		return framework.JailSubMountInfo{}, fmt.Errorf(
			"find writable jail submount containing %s: most-specific mount %s=%s is read-only",
			target,
			matches[0].Name,
			matches[0].MountPath,
		)
	}
	return matches[0], nil
}

func cleanAbsolutePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("validate absolute path %q: path must start with /", value)
	}
	for component := range strings.SplitSeq(value, "/") {
		if component == ".." {
			return "", fmt.Errorf("validate absolute path %q: parent traversal is not allowed", value)
		}
	}
	return path.Clean(value), nil
}

func describeJailSubMounts(mounts []framework.JailSubMountInfo) string {
	if len(mounts) == 0 {
		return "<none>"
	}
	descriptions := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		descriptions = append(descriptions, fmt.Sprintf("%s=%s (readOnly=%t)", mount.Name, mount.MountPath, mount.ReadOnly))
	}
	sort.Strings(descriptions)
	return strings.Join(descriptions, ", ")
}
