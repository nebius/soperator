package rebooter

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/kubeletclient"
)

type stubFetcher struct {
	podList *corev1.PodList
	err     error
	calls   int
}

func (s *stubFetcher) GetPodsOnNode(context.Context, *corev1.Node) (*corev1.PodList, error) {
	s.calls++
	return s.podList, s.err
}

func podList(names ...string) *corev1.PodList {
	list := &corev1.PodList{}
	for _, name := range names {
		list.Items = append(list.Items, corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name}})
	}
	return list
}

// nodeForServer builds the node whose status points the fetcher at an httptest TLS server.
func nodeForServer(t *testing.T, rawURL string) *corev1.Node {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)

	host, portStr, err := net.SplitHostPort(parsed.Host)
	require.NoError(t, err)

	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: host}},
			DaemonEndpoints: corev1.NodeDaemonEndpoints{
				KubeletEndpoint: corev1.DaemonEndpoint{Port: int32(port)},
			},
		},
	}
}

func newKubeletFetcher(t *testing.T) NodePodsFetcher {
	t.Helper()

	fetcher, err := NewKubeletNodePodsFetcher(nil, kubeletclient.Config{InsecureSkipTLSVerify: true})
	require.NoError(t, err)

	return fetcher
}

func TestKubeletNodePodsGetPodsOnNode(t *testing.T) {
	var gotPath string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"PodList","apiVersion":"v1","items":[{"metadata":{"name":"pod-a","namespace":"default"}}]}`))
	}))
	defer server.Close()

	list, err := newKubeletFetcher(t).GetPodsOnNode(context.Background(), nodeForServer(t, server.URL))
	require.NoError(t, err)

	assert.Equal(t, kubeletclient.PodsPath, gotPath)
	require.Len(t, list.Items, 1)
	assert.Equal(t, "pod-a", list.Items[0].Name)
}

// The kubelet authorizes /pods against nodes/proxy; without that grant it answers 403.
func TestKubeletNodePodsForbidden(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	_, err := newKubeletFetcher(t).GetPodsOnNode(context.Background(), nodeForServer(t, server.URL))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test-node")
	assert.Contains(t, err.Error(), "403")
}

// A node with no usable address fails before any request is attempted.
func TestKubeletNodePodsUnresolvableNode(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeExternalIP, Address: "203.0.113.1"}},
		},
	}

	_, err := newKubeletFetcher(t).GetPodsOnNode(context.Background(), node)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve kubelet address")
}

func TestFallbackNodePodsPrimarySucceeds(t *testing.T) {
	primary := &stubFetcher{podList: podList("pod-a")}
	secondary := &stubFetcher{podList: podList("pod-b")}

	list, err := NewFallbackNodePodsFetcher(primary, secondary).GetPodsOnNode(context.Background(), &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node"}})
	require.NoError(t, err)

	require.Len(t, list.Items, 1)
	assert.Equal(t, "pod-a", list.Items[0].Name)
	assert.Equal(t, 1, primary.calls)
	assert.Equal(t, 0, secondary.calls)
}

func TestFallbackNodePodsUsesSecondary(t *testing.T) {
	primary := &stubFetcher{err: errors.New("kubelet unreachable")}
	secondary := &stubFetcher{podList: podList("pod-b")}

	list, err := NewFallbackNodePodsFetcher(primary, secondary).GetPodsOnNode(context.Background(), &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node"}})
	require.NoError(t, err)

	require.Len(t, list.Items, 1)
	assert.Equal(t, "pod-b", list.Items[0].Name)
	assert.Equal(t, 1, secondary.calls)
}

// Both failures are reported: a drain that cannot list pods must not hide why.
func TestFallbackNodePodsBothFail(t *testing.T) {
	primary := &stubFetcher{err: errors.New("kubelet unreachable")}
	secondary := &stubFetcher{err: errors.New("proxy forbidden")}

	_, err := NewFallbackNodePodsFetcher(primary, secondary).GetPodsOnNode(context.Background(), &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kubelet unreachable")
	assert.Contains(t, err.Error(), "proxy forbidden")
}
