package kubeletclient

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

type summaryResponse struct {
	Node struct {
		NodeName string `json:"nodeName"`
	} `json:"node"`
	Pods []struct {
		PodRef struct {
			UID string `json:"uid"`
		} `json:"podRef"`
	} `json:"pods"`
}

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

func newTestClient(t *testing.T, timeout time.Duration) *Client {
	t.Helper()

	client, err := New(nil, Config{
		Timeout:               timeout,
		InsecureSkipTLSVerify: true,
	})
	require.NoError(t, err)

	return client
}

func TestClientGetSummary(t *testing.T) {
	var gotPath string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"node":{"nodeName":"test-node"},"pods":[{"podRef":{"uid":"uid-1"}}]}`))
	}))
	defer server.Close()

	host, port := splitHostPort(t, server.URL)

	var stats summaryResponse
	require.NoError(t, newTestClient(t, time.Minute).Get(context.Background(), host, port, SummaryPath, &stats))

	assert.Equal(t, SummaryPath, gotPath)
	assert.Equal(t, "test-node", stats.Node.NodeName)
	require.Len(t, stats.Pods, 1)
	assert.Equal(t, "uid-1", stats.Pods[0].PodRef.UID)
}

// The kubelet maps /pods onto the nodes/proxy sub-resource, so it decodes into a plain PodList.
func TestClientGetPods(t *testing.T) {
	var gotPath string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"PodList","apiVersion":"v1","items":[{"metadata":{"name":"pod-a","namespace":"default"}}]}`))
	}))
	defer server.Close()

	host, port := splitHostPort(t, server.URL)

	var podList corev1.PodList
	require.NoError(t, newTestClient(t, time.Minute).Get(context.Background(), host, port, PodsPath, &podList))

	assert.Equal(t, PodsPath, gotPath)
	require.Len(t, podList.Items, 1)
	assert.Equal(t, "pod-a", podList.Items[0].Name)
}

// A kubelet that authorizes against a sub-resource rejects an unauthorized caller, which is
// the expected failure when the service account lacks the sub-resource grant.
func TestClientGetForbidden(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden (user=system:serviceaccount:soperator:checks)", http.StatusForbidden)
	}))
	defer server.Close()

	host, port := splitHostPort(t, server.URL)

	err := newTestClient(t, time.Minute).Get(context.Background(), host, port, SummaryPath, &summaryResponse{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "forbidden")
}

func TestClientGetTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	host, port := splitHostPort(t, server.URL)

	err := newTestClient(t, 50*time.Millisecond).Get(context.Background(), host, port, SummaryPath, &summaryResponse{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request kubelet")
}

func TestClientDefaultPort(t *testing.T) {
	client, err := New(nil, Config{InsecureSkipTLSVerify: true})
	require.NoError(t, err)
	assert.Equal(t, int32(DefaultPort), client.DefaultPort())

	client, err = New(nil, Config{Port: 10255})
	require.NoError(t, err)
	assert.Equal(t, int32(10255), client.DefaultPort())
}

func TestAddressForNode(t *testing.T) {
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
			address, port, err := AddressForNode(tt.node)
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
