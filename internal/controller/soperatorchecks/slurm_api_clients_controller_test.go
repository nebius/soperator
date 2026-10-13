package soperatorchecks

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/slurmapi"
)

type slurmClientTransportFunc func(*http.Request) (*http.Response, error)

func (f slurmClientTransportFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSlurmAPIClientsControllerHTTPTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, 2 * time.Minute} {
		t.Run(timeout.String(), func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, slurmv1.AddToScheme(scheme))
			cluster := types.NamespacedName{Namespace: "default", Name: "cluster"}
			slurmCluster := &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: cluster.Namespace,
					Name:      cluster.Name,
				},
			}
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: cluster.Namespace,
					Name:      naming.BuildSecretSlurmRESTSecretName(cluster.Name),
				},
				Data: map[string][]byte{consts.SecretRESTJWTKeyFileName: []byte("test-signing-key")},
			}
			kubeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(slurmCluster, secret).Build()
			clients := slurmapi.NewClientSet(t.Context())
			httpClient := &http.Client{
				Timeout: timeout,
				Transport: slurmClientTransportFunc(func(req *http.Request) (*http.Response, error) {
					deadline, hasDeadline := req.Context().Deadline()
					assert.Equal(t, timeout > 0, hasDeadline)
					if timeout > 0 {
						assert.WithinDuration(t, time.Now().Add(timeout), deadline, time.Second)
					}
					assert.NotEmpty(t, req.Header.Get("X-SLURM-USER-TOKEN"))
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`{"nodes":[],"errors":[]}`)),
					}, nil
				}),
			}
			r := NewSlurmAPIClientsController(kubeClient, scheme, record.NewFakeRecorder(1), clients, httpClient)
			_, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: cluster})
			require.NoError(t, err)
			slurmClient, found := clients.GetClient(cluster)
			require.True(t, found)
			_, err = slurmClient.ListNodes(context.Background())
			require.NoError(t, err)
		})
	}
}

func TestSlurmAPIClientsControllerRemovesDeletedClusterClient(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, slurmv1.AddToScheme(scheme))

	clusterA := types.NamespacedName{Namespace: "ns-a", Name: "cluster-a"}
	clusterB := types.NamespacedName{Namespace: "ns-b", Name: "cluster-b"}

	slurmClusterA := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: clusterA.Namespace, Name: clusterA.Name},
	}
	slurmClusterB := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: clusterB.Namespace, Name: clusterB.Name},
	}

	kubeClient := clientfake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(slurmClusterA, slurmClusterB).
		Build()

	clients := slurmapi.NewClientSet(t.Context())
	r := NewSlurmAPIClientsController(kubeClient, scheme, record.NewFakeRecorder(1), clients, http.DefaultClient)

	_, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: clusterA})
	require.NoError(t, err)
	_, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: clusterB})
	require.NoError(t, err)

	_, foundA := clients.GetClient(clusterA)
	require.True(t, foundA)
	_, foundB := clients.GetClient(clusterB)
	require.True(t, foundB)

	require.NoError(t, kubeClient.Delete(t.Context(), slurmClusterA))

	_, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: clusterA})
	require.NoError(t, err)

	_, foundA = clients.GetClient(clusterA)
	assert.False(t, foundA, "deleted SlurmCluster client should be removed from ClientSet")
	_, foundB = clients.GetClient(clusterB)
	assert.True(t, foundB, "remaining SlurmCluster client must stay registered")
}
