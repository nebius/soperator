package rebooter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func TestAPIServerNodeProxyGetPodsOnNode(t *testing.T) {
	t.Helper()

	expectedPath := "/api/v1/nodes/test-node/proxy/pods"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %q", r.Method)
		}
		if r.URL.Path != expectedPath {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(&corev1.PodList{
			Items: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-a",
						Namespace: "default",
					},
				},
			},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	fetcher, err := NewAPIServerNodePodsFetcher(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatalf("create fetcher: %v", err)
	}

	podList, err := fetcher.GetPodsOnNode(context.Background(), "test-node")
	if err != nil {
		t.Fatalf("get pods on node: %v", err)
	}
	if len(podList.Items) != 1 {
		t.Fatalf("got %d pods, want 1", len(podList.Items))
	}
	if podList.Items[0].Name != "pod-a" {
		t.Fatalf("got pod %q, want %q", podList.Items[0].Name, "pod-a")
	}
}
