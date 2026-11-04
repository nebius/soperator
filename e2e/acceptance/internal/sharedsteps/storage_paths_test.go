package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestWritableJailSubMountContaining(t *testing.T) {
	mounts := []framework.JailSubMountInfo{
		{Name: "home", MountPath: "/home"},
		{Name: "image-storage", MountPath: "/mnt/local-data"},
		{Name: "nested-read-only", MountPath: "/mnt/local-data/read-only", ReadOnly: true},
	}

	mount, err := writableJailSubMountContaining("/mnt/local-data/docker", mounts)
	require.NoError(t, err)
	assert.Equal(t, mounts[1], mount)
}

func TestWritableJailSubMountContainingRejectsInvalidState(t *testing.T) {
	tests := []struct {
		name   string
		target string
		mounts []framework.JailSubMountInfo
	}{
		{name: "relative target", target: "mnt/local-data/docker"},
		{name: "parent traversal", target: "/mnt/local-data/../docker"},
		{name: "outside mounts", target: "/var/lib/docker", mounts: []framework.JailSubMountInfo{{Name: "image-storage", MountPath: "/mnt/local-data"}}},
		{name: "read-only mount", target: "/mnt/local-data/docker", mounts: []framework.JailSubMountInfo{{Name: "image-storage", MountPath: "/mnt/local-data", ReadOnly: true}}},
		{name: "nested read-only mount", target: "/mnt/local-data/read-only/docker", mounts: []framework.JailSubMountInfo{{Name: "image-storage", MountPath: "/mnt/local-data"}, {Name: "nested", MountPath: "/mnt/local-data/read-only", ReadOnly: true}}},
		{name: "duplicate path", target: "/mnt/local-data/docker", mounts: []framework.JailSubMountInfo{{Name: "one", MountPath: "/mnt/local-data"}, {Name: "two", MountPath: "/mnt/local-data"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := writableJailSubMountContaining(tt.target, tt.mounts)
			assert.Error(t, err)
		})
	}
}
