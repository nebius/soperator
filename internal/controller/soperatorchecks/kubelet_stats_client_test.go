package soperatorchecks

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// splitHostPort breaks an httptest server URL into the pieces a node status carries.
func splitHostPort(t *testing.T, rawURL string) (string, int32) {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)

	host, portStr, err := net.SplitHostPort(parsed.Host)
	require.NoError(t, err)

	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	return host, int32(port)
}

func newTestKubeletClient(t *testing.T, timeout time.Duration) *kubeletStatsClient {
	t.Helper()

	client, err := newKubeletStatsClient(nil, KubeletClientConfig{
		Timeout:               timeout,
		InsecureSkipTLSVerify: true,
	})
	require.NoError(t, err)

	return client
}

func TestKubeletStatsClientGetSummary(t *testing.T) {
	var gotPath string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"node":{"nodeName":"test-node"},"pods":[{"podRef":{"name":"p","namespace":"ns","uid":"uid-1"},"ephemeral-storage":{"usedBytes":1024}}]}`))
	}))
	defer server.Close()

	host, port := splitHostPort(t, server.URL)

	stats, err := newTestKubeletClient(t, time.Minute).GetSummary(context.Background(), host, port)
	require.NoError(t, err)

	assert.Equal(t, kubeletSummaryPath, gotPath)
	assert.Equal(t, "test-node", stats.Node.NodeName)
	require.Len(t, stats.Pods, 1)
	require.NotNil(t, stats.Pods[0].EphemeralStorage.UsedBytes)
	assert.Equal(t, uint64(1024), *stats.Pods[0].EphemeralStorage.UsedBytes)
}

// A kubelet that authorizes against nodes/stats rejects an unauthorized caller, which is
// the expected failure when the service account lacks the sub-resource grant.
func TestKubeletStatsClientGetSummaryForbidden(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden (user=system:serviceaccount:soperator:checks)", http.StatusForbidden)
	}))
	defer server.Close()

	host, port := splitHostPort(t, server.URL)

	_, err := newTestKubeletClient(t, time.Minute).GetSummary(context.Background(), host, port)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "forbidden")
}

func TestKubeletStatsClientGetSummaryTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	host, port := splitHostPort(t, server.URL)

	_, err := newTestKubeletClient(t, 50*time.Millisecond).GetSummary(context.Background(), host, port)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request kubelet stats")
}

func TestKubeletStatsClientDefaultPort(t *testing.T) {
	client, err := newKubeletStatsClient(nil, KubeletClientConfig{InsecureSkipTLSVerify: true})
	require.NoError(t, err)
	assert.Equal(t, int32(DefaultKubeletPort), client.defaultPort)

	client, err = newKubeletStatsClient(nil, KubeletClientConfig{Port: 10255})
	require.NoError(t, err)
	assert.Equal(t, int32(10255), client.defaultPort)
}

func TestKubeletAddressForNode(t *testing.T) {
	tests := []struct {
		name        string
		node        *corev1.Node
		wantAddress string
		wantPort    int32
		wantErr     bool
	}{
		{
			name: "internal ip preferred over hostname",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeHostName, Address: "node-1.internal"},
						{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
					},
					DaemonEndpoints: corev1.NodeDaemonEndpoints{
						KubeletEndpoint: corev1.DaemonEndpoint{Port: 10250},
					},
				},
			},
			wantAddress: "10.0.0.1",
			wantPort:    10250,
		},
		{
			name: "hostname when no internal ip",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node-2"},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeExternalIP, Address: "203.0.113.1"},
						{Type: corev1.NodeHostName, Address: "node-2.internal"},
					},
				},
			},
			wantAddress: "node-2.internal",
			wantPort:    0,
		},
		{
			name: "no usable address",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node-3"},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeExternalIP, Address: "203.0.113.1"},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			address, port, err := kubeletAddressForNode(tt.node)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantAddress, address)
			assert.Equal(t, tt.wantPort, port)
		})
	}
}
