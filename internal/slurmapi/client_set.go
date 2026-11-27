package slurmapi

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
)

// nodeCacheEntry pairs a running NodeCache with the cancel func of its refresh goroutine,
// so the cache can be stopped on its own when its cluster goes away.
type nodeCacheEntry struct {
	cache  *NodeCache
	cancel context.CancelFunc
}

type ClientSet struct {
	slurmAPIClients map[types.NamespacedName]Client
	nodeCaches      map[types.NamespacedName]nodeCacheEntry
	lifetimeCtx     context.Context
	mux             *sync.Mutex
}

func NewClientSet(lifetimeCtx context.Context) *ClientSet {
	return &ClientSet{
		slurmAPIClients: make(map[types.NamespacedName]Client),
		nodeCaches:      make(map[types.NamespacedName]nodeCacheEntry),
		lifetimeCtx:     lifetimeCtx,
		mux:             &sync.Mutex{},
	}
}

func (cs *ClientSet) AddClient(name types.NamespacedName, client Client) {
	cs.mux.Lock()
	defer cs.mux.Unlock()

	cs.slurmAPIClients[name] = client
}

// RemoveClient forgets the client registered for the given cluster and stops its NodeCache
// if one was started. Removing a cluster that is not registered is a no-op.
func (cs *ClientSet) RemoveClient(name types.NamespacedName) {
	cs.mux.Lock()
	defer cs.mux.Unlock()

	delete(cs.slurmAPIClients, name)

	if entry, exists := cs.nodeCaches[name]; exists {
		entry.cancel()
		delete(cs.nodeCaches, name)
	}
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

	if entry, exists := cs.nodeCaches[name]; exists {
		return entry.cache
	}

	client, exists := cs.slurmAPIClients[name]
	if !exists {
		return nil
	}

	ctx, cancel := context.WithCancel(cs.lifetimeCtx)
	nc := StartNodeCache(ctx, client, refreshInterval, logger)
	cs.nodeCaches[name] = nodeCacheEntry{cache: nc, cancel: cancel}
	return nc
}

func (cs *ClientSet) GetNodeCache(name types.NamespacedName) (*NodeCache, bool) {
	cs.mux.Lock()
	defer cs.mux.Unlock()

	entry, found := cs.nodeCaches[name]
	return entry.cache, found
}
