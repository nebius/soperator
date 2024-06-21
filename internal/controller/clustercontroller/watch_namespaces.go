package clustercontroller

import (
	"slices"
	"strings"
)

type WatchNamespaces []string

func NewWatchNamespaces(env string) WatchNamespaces {
	env = strings.Trim(env, " ")
	namespaces := strings.Split(env, " ")
	return namespaces
}

const allNamespaces = "*"

func (wn WatchNamespaces) IsWatched(namespace string) bool {
	if len(wn) == 0 || slices.Contains(wn, allNamespaces) {
		return true
	}
	return slices.Contains(wn, namespace)
}
