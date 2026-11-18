package framework

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

const reservationPollInterval = time.Second
const reservationOperationTimeout = 2 * time.Minute

// readCache coalesces reads without coupling their lifetime to any one waiter.
// Mutation confirmation and recovery deliberately bypass it.
type readCache[T any] struct {
	mu      sync.Mutex
	entries map[string]*cachedRead[T]
}

type cachedRead[T any] struct {
	done    chan struct{}
	value   T
	err     error
	expires time.Time
}

func (c *readCache[T]) get(ctx context.Context, key string, ttl time.Duration, read func(context.Context) (T, error)) (T, error) {
	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[string]*cachedRead[T])
	}
	entry := c.entries[key]
	if entry == nil || (!entry.expires.IsZero() && time.Now().After(entry.expires)) {
		if len(c.entries) >= 256 {
			clear(c.entries)
		}
		entry = &cachedRead[T]{done: make(chan struct{})}
		c.entries[key] = entry
		go func(entry *cachedRead[T]) {
			readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reservationOperationTimeout)
			defer cancel()
			value, err := read(readCtx)
			c.mu.Lock()
			defer c.mu.Unlock()
			entry.value, entry.err = value, err
			if ttl > 0 {
				entry.expires = time.Now().Add(ttl)
			}
			if err != nil && c.entries[key] == entry {
				delete(c.entries, key)
			}
			close(entry.done)
		}(entry)
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case <-entry.done:
		return entry.value, entry.err
	}
}

func (c *readCache[T]) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
}

func transientDiscoveryError(err error) bool {
	if err == nil {
		return false
	}
	var permanent *PermanentError
	if errors.As(err, &permanent) {
		return false
	}
	message := commandFailureText(err)
	if strings.Contains(message, "container not found") || strings.Contains(message, "unable to contact slurm controller (connect failure)") {
		return true
	}
	for _, permanent := range []string{"forbidden", "unauthorized", "permission denied", "invalid", "not found", "no nodesets", "do not match", "parse", "certificate"} {
		if strings.Contains(message, permanent) {
			return false
		}
	}
	for _, temporary := range []string{"connection refused", "connection reset", "connection timed out", "i/o timeout", "unexpected eof", "temporarily unavailable", "service unavailable", "too many requests", "tls handshake timeout", "socket timed out"} {
		if strings.Contains(message, temporary) {
			return true
		}
	}
	return false
}

func discoveryRead(ctx context.Context, run func(context.Context) (string, error)) (string, error) {
	return retryIf(ctx, 3, time.Second, func(_ string, err error) bool { return transientDiscoveryError(err) }, run)
}
