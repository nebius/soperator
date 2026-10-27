package controllerconfig

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
)

func TestCacheByObjectFiltersSecretListAndWatch(t *testing.T) {
	const namespace = "slurm"
	const wantSelector = "type!=helm.sh/release.v1,type!=example.com/archive"
	secrets := []corev1.Secret{
		{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{Name: "release", Namespace: namespace, ResourceVersion: "1"},
			Type:       "helm.sh/release.v1",
		},
		{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{Name: "archive", Namespace: namespace, ResourceVersion: "1"},
			Type:       "example.com/archive",
		},
		{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{
				Name: "jwt-key", Namespace: namespace, ResourceVersion: "1",
				Labels: map[string]string{"app.kubernetes.io/managed-by": "Helm"},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{"key": []byte("initial")},
		},
	}
	listRequests := make(chan string, 10)
	watchRequests := make(chan string, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, "/api/v1/namespaces/"+namespace+"/secrets", r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("sendInitialEvents") == "true" {
			http.Error(w, "watch-list is unsupported by this test server", http.StatusBadRequest)
			return
		}
		selectorValue := r.URL.Query().Get("fieldSelector")
		selector, err := fields.ParseSelector(selectorValue)
		if !assert.NoError(t, err) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		encoder := json.NewEncoder(w)
		if r.URL.Query().Get("watch") == "true" {
			watchRequests <- selectorValue
			updated := secrets[2].DeepCopy()
			updated.ResourceVersion = "2"
			updated.Data["key"] = []byte("rotated")
			if selector.Matches(fields.Set{"type": string(updated.Type)}) {
				assert.NoError(t, encoder.Encode(map[string]any{"type": watch.Modified, "object": updated}))
			}
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		listRequests <- selectorValue
		list := corev1.SecretList{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "SecretList"},
			ListMeta: metav1.ListMeta{ResourceVersion: "1"},
		}
		for _, secret := range secrets {
			if selector.Matches(fields.Set{"type": string(secret.Type)}) {
				list.Items = append(list.Items, secret)
			}
		}
		assert.NoError(t, encoder.Encode(list))
	}))
	defer server.Close()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("Secret"), meta.RESTScopeNamespace)
	mapper.Add(corev1.SchemeGroupVersion.WithKind("Node"), meta.RESTScopeRoot)
	secretCache, err := cache.New(&rest.Config{Host: server.URL}, cache.Options{
		Scheme:            scheme,
		Mapper:            mapper,
		DefaultNamespaces: map[string]cache.Config{namespace: {}},
		ByObject:          CacheByObject([]string{"helm.sh/release.v1", "example.com/archive"}, logr.Discard()),
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = secretCache.GetInformer(ctx, &corev1.Secret{})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- secretCache.Start(ctx) }()
	defer func() {
		cancel()
		require.NoError(t, <-done)
	}()
	require.True(t, secretCache.WaitForCacheSync(ctx))

	for _, requests := range []<-chan string{listRequests, watchRequests} {
		select {
		case selector := <-requests:
			require.Equal(t, wantSelector, selector)
		case <-ctx.Done():
			t.Fatal("timed out waiting for Secret LIST/WATCH requests")
		}
	}
	for _, name := range []string{"release", "archive"} {
		err := secretCache.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &corev1.Secret{})
		require.True(t, apierrors.IsNotFound(err), "ignored Secret %s must be absent from the cache", name)
	}
	require.Eventually(t, func() bool {
		secret := &corev1.Secret{}
		if err := secretCache.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "jwt-key"}, secret); err != nil {
			return false
		}
		return string(secret.Data["key"]) == "rotated"
	}, 5*time.Second, 10*time.Millisecond, "application Secret updates must remain observable")
}
