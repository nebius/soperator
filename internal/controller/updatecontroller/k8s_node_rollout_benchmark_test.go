package updatecontroller

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
)

// Use the real controller-runtime cache; the test source only replaces its initial list/watch.
func testK8sNodeCache(tb testing.TB, nodes []corev1.Node) client.Reader {
	tb.Helper()
	scheme := runtime.NewScheme()
	require.NoError(tb, corev1.AddToScheme(scheme))
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("Node"), meta.RESTScopeRoot)
	nodeList := &corev1.NodeList{ListMeta: metav1.ListMeta{ResourceVersion: "1"}, Items: nodes}
	nodeWatch := watch.NewRaceFreeFake()
	nodeCache, err := cache.New(&rest.Config{Host: "https://unused.invalid"}, cache.Options{
		Scheme:   scheme,
		Mapper:   mapper,
		ByObject: controllerconfig.NodeCacheByObject(),
		NewInformer: func(_ toolscache.ListerWatcher, obj runtime.Object, resync time.Duration, indexes toolscache.Indexers) toolscache.SharedIndexInformer {
			return toolscache.NewSharedIndexInformer(&toolscache.ListWatch{
				ListFunc: func(metav1.ListOptions) (runtime.Object, error) { return nodeList.DeepCopy(), nil },
				WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
					if opts.SendInitialEvents != nil && *opts.SendInitialEvents {
						return nil, errors.New("watch-list is unsupported by this test source")
					}
					return nodeWatch, nil
				},
			}, obj, resync, indexes)
		},
	})
	require.NoError(tb, err)
	ctx, cancel := context.WithCancel(context.Background())
	_, err = nodeCache.GetInformer(ctx, &corev1.Node{})
	require.NoError(tb, err)
	done := make(chan error, 1)
	go func() { done <- nodeCache.Start(ctx) }()
	tb.Cleanup(func() {
		cancel()
		require.NoError(tb, <-done)
	})
	syncCtx, syncCancel := context.WithTimeout(ctx, 10*time.Second)
	defer syncCancel()
	require.True(tb, nodeCache.WaitForCacheSync(syncCtx))
	return nodeCache
}

func BenchmarkGetK8sNodeCordonStates(b *testing.B) {
	for _, count := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("workers=%d", count), func(b *testing.B) {
			nodes := make([]corev1.Node, count)
			pods := make([]corev1.Pod, count)
			for i := range nodes {
				name := fmt.Sprintf("node-%d", i)
				nodes[i] = corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
						"kubernetes.io/hostname": name, "topology.kubernetes.io/zone": "zone-1",
					}},
					Spec:   corev1.NodeSpec{Unschedulable: i%100 == 0},
					Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
				}
				pods[i].Spec.NodeName = name
			}
			reader := testK8sNodeCache(b, nodes)
			r := &RollingUpdateReconciler{Reconciler: &reconciler.Reconciler{Client: nodeCacheReader{Reader: reader}}}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				states, err := r.getK8sNodeCordonStates(context.Background(), pods)
				if err != nil || len(states) != count {
					b.Fatalf("read cordon states: count=%d, err=%v", len(states), err)
				}
			}
		})
	}
}

type nodeCacheReader struct {
	client.Client
	client.Reader
}

func (c nodeCacheReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return c.Reader.Get(ctx, key, obj, opts...)
}

func (c nodeCacheReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return c.Reader.List(ctx, list, opts...)
}
