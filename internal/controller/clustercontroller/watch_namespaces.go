package clustercontroller

import (
	"slices"
	"strings"
)

type WatchNamespaces []string

func NewWatchNamespaces(env string) WatchNamespaces {
	namespacesFromEnv := strings.Split(env, ",")
	var watchNamespaces []string
	for _, ns := range namespacesFromEnv {
		ns = strings.TrimSpace(ns)
		if len(ns) > 0 {
			watchNamespaces = append(watchNamespaces, ns)
		}
	}
	return watchNamespaces
}

const allNamespaces = "*"

func (wn WatchNamespaces) IsWatched(namespace string) bool {
	if len(wn) == 0 || slices.Contains(wn, allNamespaces) {
		return true
	}
	return slices.Contains(wn, namespace)
}
