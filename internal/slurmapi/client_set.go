package slurmapi

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
)

type ClientSet struct {
	slurmAPIClients map[types.NamespacedName]Client
	nodeCaches      map[types.NamespacedName]*NodeCache
	lifetimeCtx     context.Context
	mux             *sync.Mutex
}

func NewClientSet(lifetimeCtx context.Context) *ClientSet {
	return &ClientSet{
		slurmAPIClients: make(map[types.NamespacedName]Client),
		nodeCaches:      make(map[types.NamespacedName]*NodeCache),
		lifetimeCtx:     lifetimeCtx,
		mux:             &sync.Mutex{},
	}
}

func (cs *ClientSet) AddClient(name types.NamespacedName, client Client) {
	cs.mux.Lock()
	defer cs.mux.Unlock()

	cs.slurmAPIClients[name] = client
}

func (cs *ClientSet) GetClient(name types.NamespacedName) (Client, bool) {
	cs.mux.Lock()
	defer cs.mux.Unlock()

	client, found := cs.slurmAPIClients[name]
	return client, found
}

func (cs *ClientSet) GetClients() (slurmAPIClients map[types.NamespacedName]Client) {
	cs.mux.Lock()
	defer cs.mux.Unlock()

	return maps.Clone(cs.slurmAPIClients)
}

// EnsureNodeCache creates and starts a NodeCache for the given cluster if one does
// not already exist. Returns nil if no client is registered for this cluster.
func (cs *ClientSet) EnsureNodeCache(name types.NamespacedName, refreshInterval time.Duration, logger logr.Logger) *NodeCache {
	cs.mux.Lock()
	defer cs.mux.Unlock()

	if nc, exists := cs.nodeCaches[name]; exists {
		return nc
	}

	client, exists := cs.slurmAPIClients[name]
	if !exists {
		return nil
	}

	nc := StartNodeCache(cs.lifetimeCtx, client, refreshInterval, logger)
	cs.nodeCaches[name] = nc
	return nc
}

func (cs *ClientSet) GetNodeCache(name types.NamespacedName) (*NodeCache, bool) {
	cs.mux.Lock()
	defer cs.mux.Unlock()

	nc, found := cs.nodeCaches[name]
	return nc, found
}
