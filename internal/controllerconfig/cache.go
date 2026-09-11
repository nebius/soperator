package controllerconfig

import (
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
