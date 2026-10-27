package framework

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type workerReadinessRuntime struct {
	Runtime
	state string
	pods  string
	hosts func() string
}

func (r *workerReadinessRuntime) Controller() CommandScope {
	return NewCommandScope(func(_ context.Context, command string) (string, error) {
		if command == `sinfo -hN -p main -o '%N'` {
			return "worker-0\nworker-1", nil
		}
		return fmt.Sprintf("NodeName=worker-0 State=%s\nNodeName=worker-1 State=IDLE", r.state), nil
	})
}

func (r *workerReadinessRuntime) Kubectl() ArgsScope {
	return NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		switch strings.Join(args[:2], " ") {
		case "get nodesets":
			return `{"items":[{"metadata":{"name":"worker"},"spec":{"replicas":2}}]}`, nil
		case "get pods":
			return r.pods, nil
		case "get nodes":
			return r.hosts(), nil
		default:
			return "", fmt.Errorf("unexpected kubectl command %v", args)
		}
	})
}

func (*workerReadinessRuntime) Logf(string, ...any) {}

func workerReadinessFixture() *workerReadinessRuntime {
	return &workerReadinessRuntime{
		state: "IDLE",
		pods: `{"items":[
			{"metadata":{"name":"worker-0"},"spec":{"hostname":"worker-0","nodeName":"host-0"},"status":{"conditions":[{"type":"Ready","status":"True"}]}},
			{"metadata":{"name":"worker-1"},"spec":{"hostname":"worker-1","nodeName":"host-1"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}
		]}`,
	}
}

func workerHostsFixture(status string) string {
	return fmt.Sprintf(`{"items":[
		{"metadata":{"name":"host-0"},"status":{"conditions":[{"type":"Ready","status":%q}]}},
		{"metadata":{"name":"host-1"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}
	]}`, status)
}

func TestWorkerSelectorRequiresReadyPodAndHostForActiveWorkers(t *testing.T) {
	for _, tt := range []struct {
		name, status, state, pods, hosts, want string
	}{
		{name: "ready", status: "True", want: "worker-0"},
		{name: "host not ready", status: "False", want: "worker-1"},
		{name: "host unknown", status: "Unknown", want: "worker-1"},
		{name: "host missing", hosts: `{"items":[{"metadata":{"name":"host-1"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`, want: "worker-1"},
		{name: "host condition missing", hosts: `{"items":[{"metadata":{"name":"host-0"}},{"metadata":{"name":"host-1"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`, want: "worker-1"},
		{name: "pod not ready", status: "True", pods: `{"items":[{"spec":{"hostname":"worker-0","nodeName":"host-0"}},{"spec":{"hostname":"worker-1","nodeName":"host-1"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`, want: "worker-1"},
		{name: "powered down remains schedulable", state: "IDLE+CLOUD+POWERED_DOWN", pods: `{"items":[]}`, hosts: `{"items":[]}`, want: "worker-0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runtime := workerReadinessFixture()
			runtime.hosts = func() string {
				if tt.hosts != "" {
					return tt.hosts
				}
				return workerHostsFixture(tt.status)
			}
			if tt.state != "" {
				runtime.state = tt.state
			}
			if tt.pods != "" {
				runtime.pods = tt.pods
			}
			ctx := WithWorkerReservation(t.Context(), NewWorkerPool(), t.Name())
			ctx = WithWorkerAcquireTimeout(ctx, time.Second)
			selector := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator")
			workers, err := selector.PickWorkers(ctx, 1)
			require.NoError(t, err)
			require.Len(t, workers, 1)
			assert.Equal(t, tt.want, workers[0].Name)
		})
	}
}

func TestWorkerSelectorWaitsForHostRecoveryWithinSharedAcquisition(t *testing.T) {
	for _, recoverHost := range []bool{false, true} {
		t.Run(fmt.Sprintf("recover=%t", recoverHost), func(t *testing.T) {
			runtime := workerReadinessFixture()
			var ready atomic.Bool
			observed := make(chan bool, 2)
			runtime.hosts = func() string {
				status := ready.Load()
				select {
				case observed <- status:
				default:
				}
				if status {
					return workerHostsFixture("True")
				}
				return workerHostsFixture("False")
			}
			pool := NewWorkerPool()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			ctx = WithWorkerReservation(ctx, pool, t.Name())
			selector := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator")
			result := make(chan error, 1)
			go func() {
				_, err := selector.Acquire(ctx, WorkerRequirements{Count: 1, Names: []string{"worker-0"}})
				result <- err
			}()
			select {
			case status := <-observed:
				require.False(t, status)
			case <-ctx.Done():
				t.Fatal("worker acquisition did not query host readiness")
			}
			require.Eventually(t, func() bool {
				pool.mu.Lock()
				defer pool.mu.Unlock()
				return len(pool.waiting) == 1 && len(pool.waiting[0].candidates) == 1
			}, time.Second, time.Millisecond)
			pool.mu.Lock()
			assert.Empty(t, pool.owners)
			if recoverHost {
				ready.Store(true)
			} else {
				cancel()
			}
			pool.notify()
			pool.mu.Unlock()
			if recoverHost {
				require.NoError(t, <-result)
				pool.mu.Lock()
				assert.Same(t, workerReservation(ctx), pool.owners["worker-0"])
				pool.mu.Unlock()
			} else {
				require.ErrorIs(t, <-result, context.Canceled)
				pool.mu.Lock()
				assert.Empty(t, pool.owners)
				assert.Empty(t, pool.waiting)
				pool.mu.Unlock()
			}
		})
	}
}
