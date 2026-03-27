package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestIsContainerCreateRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		want bool
	}{
		{name: "plain", path: "/containers/create", want: true},
		{name: "versioned", path: "/v1.50/containers/create", want: true},
		{name: "other", path: "/v1.50/containers/json", want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequest(http.MethodPost, "http://docker"+tc.path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}

			if got := isContainerCreateRequest(req); got != tc.want {
				t.Fatalf("isContainerCreateRequest(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestSetDefaultCgroupParent(t *testing.T) {
	t.Parallel()

	body := []byte(`{"Image":"busybox","HostConfig":{"AutoRemove":true}}`)
	updated, changed, err := setDefaultCgroupParent(body, "custom.slice")
	if err != nil {
		t.Fatalf("setDefaultCgroupParent: %v", err)
	}
	if !changed {
		t.Fatal("expected payload to change")
	}

	var payload map[string]any
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("unmarshal updated body: %v", err)
	}

	hostConfig, ok := payload["HostConfig"].(map[string]any)
	if !ok {
		t.Fatal("HostConfig missing from payload")
	}

	if got := hostConfig["CgroupParent"]; got != "custom.slice" {
		t.Fatalf("CgroupParent = %v, want %q", got, "custom.slice")
	}
}

func TestApplyDefaultCgroupParentPreservesExplicitValue(t *testing.T) {
	t.Parallel()

	body := []byte(`{"Image":"busybox","HostConfig":{"CgroupParent":"already.set"}}`)
	req, err := http.NewRequest(http.MethodPost, "http://docker/v1.50/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Cgroup-Parent", "custom.slice")

	if err := applyDefaultCgroupParent(req); err != nil {
		t.Fatalf("applyDefaultCgroupParent: %v", err)
	}

	updatedBody := make([]byte, req.ContentLength)
	if _, err := req.Body.Read(updatedBody); err != nil {
		t.Fatalf("read body: %v", err)
	}

	if string(updatedBody) != string(body) {
		t.Fatalf("body changed unexpectedly: %s", updatedBody)
	}
}
