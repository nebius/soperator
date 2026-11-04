package controllerconfig

import (
	"os"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const IgnoredSecretTypesEnv = "SLURM_OPERATOR_IGNORED_SECRET_TYPES"

// IgnoredSecretTypesFromEnv returns the configured exclusions. An unset or empty variable disables exclusions.
func IgnoredSecretTypesFromEnv() []string {
	value := os.Getenv(IgnoredSecretTypesEnv)

	var ignoredTypes []string
	seen := make(map[string]struct{})
	for _, secretType := range strings.Split(value, ",") {
		secretType = strings.TrimSpace(secretType)
		if secretType == "" {
			continue
		}
		if _, exists := seen[secretType]; exists {
			continue
		}
		seen[secretType] = struct{}{}
		ignoredTypes = append(ignoredTypes, secretType)
	}
	return ignoredTypes
}

// CacheByObject filters ignored Secret types at the API server and trims cached Nodes.
func CacheByObject(ignoredSecretTypes []string, logger logr.Logger) map[client.Object]cache.ByObject {
	byObject := NodeCacheByObject()
	if len(ignoredSecretTypes) == 0 {
		logger.V(1).Info("Secret type filtering is disabled")
		return byObject
	}

	var selectors []fields.Selector
	for _, secretType := range ignoredSecretTypes {
		selectors = append(selectors, fields.OneTermNotEqualSelector("type", secretType))
	}
	selector := fields.AndSelectors(selectors...)
	byObject[&corev1.Secret{}] = cache.ByObject{Field: selector}
	logger.V(1).Info("Skipping Secret types in cache LIST/WATCH requests",
		"ignoredSecretTypes", ignoredSecretTypes, "fieldSelector", selector.String())
	return byObject
}

// NodeCacheByObject trims Node objects on their way into the manager cache.
//
// Managers that watch Nodes hold every Node in the cluster, and at a few thousand nodes the two
// heaviest fields on a Node object are status.images (the kubelet reports every image on disk, with
// all of its tags) and metadata.managedFields. Nothing in soperator reads either, so dropping them
// before the object is stored keeps the Node cache to the parts controllers actually use.
func NodeCacheByObject() map[client.Object]cache.ByObject {
	return map[client.Object]cache.ByObject{
		&corev1.Node{}: {Transform: TrimNodeForCache},
	}
}

func TrimNodeForCache(obj any) (any, error) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		return obj, nil
	}

	node.ManagedFields = nil
	node.Status.Images = nil

	return node, nil
}
