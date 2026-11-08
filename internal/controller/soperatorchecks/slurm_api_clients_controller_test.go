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
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

type slurmClientTransportFunc func(*http.Request) (*http.Response, error)

func (f slurmClientTransportFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newSlurmAPIClientsTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, slurmv1.AddToScheme(scheme))
	return scheme
}

func TestSlurmAPIClientsControllerHTTPTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, 2 * time.Minute} {
		t.Run(timeout.String(), func(t *testing.T) {
			scheme := newSlurmAPIClientsTestScheme(t)
			cluster := types.NamespacedName{Namespace: "default", Name: "cluster"}
			slurmCluster := &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: cluster.Namespace, Name: cluster.Name},
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

func TestSlurmAPIClientsControllerRemovesClientOfDeletedCluster(t *testing.T) {
	scheme := newSlurmAPIClientsTestScheme(t)
	deleted := types.NamespacedName{Namespace: "default", Name: "deleted"}
	remaining := types.NamespacedName{Namespace: "default", Name: "remaining"}
	remainingCluster := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: remaining.Namespace, Name: remaining.Name},
	}
	kubeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(remainingCluster).Build()

	clients := slurmapi.NewClientSet(t.Context())
	clients.AddClient(deleted, slurmapifake.NewMockClient(t))
	clients.AddClient(remaining, slurmapifake.NewMockClient(t))

	r := NewSlurmAPIClientsController(kubeClient, scheme, record.NewFakeRecorder(1), clients, nil)
	_, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: deleted})
	require.NoError(t, err)

	_, found := clients.GetClient(deleted)
	require.False(t, found, "client of the deleted cluster must be removed")
	_, found = clients.GetClient(remaining)
	require.True(t, found, "client of a cluster that still exists must stay registered")
}
