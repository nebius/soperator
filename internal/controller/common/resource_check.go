package common

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type ResourceCheck struct {
	Check     bool
	Objects   []client.Object
	Predicate predicate.Predicate
}
