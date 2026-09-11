package slurmapi

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
)

const (
	nodeCacheRequestTimeout   = 2 * time.Minute
	nodeCacheStalenessTimeout = 3 * time.Minute
)

// cacheSnapshot is an immutable point-in-time copy of the node list with its
// capture timestamp. Stored and read via atomic.Pointer — no locks required.
type cacheSnapshot struct {
	nodes map[string]Node
	at    time.Time
}

// NodeCache maintains a continuously-refreshed in-memory copy of all Slurm nodes
// for a single cluster. A background goroutine calls ListNodes every refreshInterval
// and stores the result atomically so readers never block. If the cache goes stale
// (no successful refresh for nodeCacheStalenessTimeout), the snapshot is cleared so
// callers receive proper "not found" errors instead of operating on outdated state.
type NodeCache struct {
	client          Client
	logger          logr.Logger
	refreshInterval time.Duration

	snapshot atomic.Pointer[cacheSnapshot]

	ready chan struct{}
}

// StartNodeCache starts a background goroutine that refreshes the node list every
// refreshInterval. Use WaitReady to block until the first successful refresh.
func StartNodeCache(ctx context.Context, client Client, refreshInterval time.Duration, logger logr.Logger) *NodeCache {
	nc := &NodeCache{
		client:          client,
		logger:          logger,
		refreshInterval: refreshInterval,
		ready:           make(chan struct{}),
	}
	go nc.run(ctx)
	return nc
}

// WaitReady blocks until the first successful ListNodes refresh completes or ctx is done.
func (nc *NodeCache) WaitReady(ctx context.Context) error {
	select {
	case <-nc.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// GetNode returns the cached node with the given name, or false if not found.
func (nc *NodeCache) GetNode(name string) (Node, bool) {
	s := nc.snapshot.Load()
	if s == nil {
		return Node{}, false
	}
	node, ok := s.nodes[name]
	return node, ok
}

func (nc *NodeCache) run(ctx context.Context) {
	ticker := time.NewTicker(nc.refreshInterval)
	defer ticker.Stop()

	// Initial refresh loop: keep retrying until one refresh succeeds,
	// then signal readiness and move to the steady-state loop.
	for {
		if nc.refresh(ctx) {
			close(nc.ready)
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}

	// Steady-state loop: refresh on every tick.
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nc.refresh(ctx)
		}
	}
}

// refresh performs one ListNodes call and updates the snapshot. Returns true on success.
func (nc *NodeCache) refresh(ctx context.Context) bool {
	if s := nc.snapshot.Load(); s != nil && time.Since(s.at) > nodeCacheStalenessTimeout {
		nc.logger.Error(nil, "Slurm node cache is stale, clearing cached data",
			"lastRefreshTime", s.at,
			"elapsed", time.Since(s.at),
		)
		nc.snapshot.Store(nil)
		// After clearing, s.at is gone — the stale check won't fire again until
		// the next successful refresh followed by another full staleness period.
	}

	reqCtx, cancel := context.WithTimeout(ctx, nodeCacheRequestTimeout)
	defer cancel()

	nodes, err := nc.client.ListNodes(reqCtx)
	if err != nil {
		nc.logger.Error(err, "Failed to list Slurm nodes")
		return false
	}

	nodeMap := make(map[string]Node, len(nodes))
	for _, n := range nodes {
		nodeMap[n.Name] = n
	}
	nc.snapshot.Store(&cacheSnapshot{nodes: nodeMap, at: time.Now()})
	return true
}
