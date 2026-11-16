package framework

import (
	"context"
	"errors"
	"sync"
)

type workerCleanupKey struct{}

type workerCleanup struct {
	mu  sync.Mutex
	err error
}

// WithWorkerCleanup tracks scenario restoration failures without enabling native
// reservations. FinishWorkerReservation reports them after scenario cleanup.
func WithWorkerCleanup(ctx context.Context) context.Context {
	return context.WithValue(ctx, workerCleanupKey{}, &workerCleanup{})
}

func workerCleanupFromContext(ctx context.Context) *workerCleanup {
	cleanup, _ := ctx.Value(workerCleanupKey{}).(*workerCleanup)
	return cleanup
}

func (c *workerCleanup) record(err error) {
	if c == nil || err == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = errors.Join(c.err, err)
}

func (c *workerCleanup) result() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}
