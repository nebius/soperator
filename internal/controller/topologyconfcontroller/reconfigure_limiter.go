package topologyconfcontroller

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/types"
)

// reconfigureLimiter is a token bucket per cluster for the Slurm reconfigures that deferrable
// topology changes cause. It lives in memory, so an operator restart or a leader change refills it.
// A zero interval disables the limit.
type reconfigureLimiter struct {
	interval time.Duration
	burst    int

	mu       sync.Mutex
	limiters map[types.NamespacedName]*rate.Limiter
}

// hasToken only checks the bucket. The token is taken separately, once the change is published, so a
// failed publish does not spend one.
func (l *reconfigureLimiter) hasToken(key types.NamespacedName, now time.Time) bool {
	if l.interval <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limiter(key).TokensAt(now) >= 1
}

func (l *reconfigureLimiter) take(key types.NamespacedName, now time.Time) {
	if l.interval <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limiter(key).AllowN(now, 1)
}

func (l *reconfigureLimiter) limiter(key types.NamespacedName) *rate.Limiter {
	limiter := l.limiters[key]
	if limiter == nil {
		if l.limiters == nil {
			l.limiters = make(map[types.NamespacedName]*rate.Limiter)
		}
		limiter = rate.NewLimiter(rate.Every(l.interval), l.burst)
		l.limiters[key] = limiter
	}
	return limiter
}

func (l *reconfigureLimiter) forget(key types.NamespacedName) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.limiters, key)
}
