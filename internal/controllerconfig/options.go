package controllerconfig

import (
	"sync"
	"time"

	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	optionsInit    sync.Once
	defaultOptions *controller.Options
)

func ControllerOptions(maxConcurrency int, cacheSyncTimeout time.Duration) controller.Options {
	rateLimiters := workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](2*time.Second, 2*time.Minute)
	optionsInit.Do(func() {
		defaultOptions = &controller.Options{
			RateLimiter:             rateLimiters,
			CacheSyncTimeout:        cacheSyncTimeout,
			MaxConcurrentReconciles: maxConcurrency,
		}
	})
	return *defaultOptions
}
