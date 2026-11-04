package jailedpayload_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/utils/jailedpayload"
)

func testConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		Data:       map[string]string{"/etc/slurm/topology.yaml": "topology: []\n"},
		BinaryData: map[string][]byte{"/etc/slurm/key.bin": {0x01, 0x02}},
	}
}

func Test_Make_Basic(t *testing.T) {
	payload, err := jailedpayload.Make(&v1alpha1.JailedConfigSpec{}, testConfigMap())
	require.NoError(t, err)
	assert.Equal(t, map[string]jailedpayload.File{
		"/etc/slurm/topology.yaml": {Data: []byte("topology: []\n"), Mode: 0o644},
		"/etc/slurm/key.bin":       {Data: []byte{0x01, 0x02}, Mode: 0o644},
	}, payload)
}

func Test_Make_Items(t *testing.T) {
	spec := &v1alpha1.JailedConfigSpec{
		DefaultMode: ptr.To[int32](0o640),
		Items: []corev1.KeyToPath{
			{Key: "/etc/slurm/topology.yaml", Path: "/mnt/jail/etc/slurm/topology.yaml"},
			{Key: "/etc/slurm/key.bin", Path: "/mnt/jail/etc/slurm/key.bin", Mode: ptr.To[int32](0o600)},
		},
	}
	payload, err := jailedpayload.Make(spec, testConfigMap())
	require.NoError(t, err)
	assert.Equal(t, map[string]jailedpayload.File{
		"/mnt/jail/etc/slurm/topology.yaml": {Data: []byte("topology: []\n"), Mode: 0o640},
		"/mnt/jail/etc/slurm/key.bin":       {Data: []byte{0x01, 0x02}, Mode: 0o600},
	}, payload)
}

func Test_Make_Errors(t *testing.T) {
	tests := []struct {
		name string
		spec v1alpha1.JailedConfigSpec
	}{
		{name: "missing key", spec: v1alpha1.JailedConfigSpec{
			Items: []corev1.KeyToPath{{Key: "missing", Path: "/etc/slurm/missing"}},
		}},
		{name: "relative path", spec: v1alpha1.JailedConfigSpec{
			Items: []corev1.KeyToPath{{Key: "/etc/slurm/topology.yaml", Path: "etc/slurm/topology.yaml"}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := jailedpayload.Make(&tt.spec, testConfigMap())
			assert.Error(t, err)
		})
	}
}

func Test_Hash_Basic(t *testing.T) {
	payload := map[string]jailedpayload.File{
		"/etc/slurm/topology.yaml": {Data: []byte("topology: []\n"), Mode: 0o644},
	}
	hash := jailedpayload.Hash(payload)
	assert.Len(t, hash, 64)
	assert.Equal(t, hash, jailedpayload.Hash(payload))
	assert.NotEqual(t, hash, jailedpayload.Hash(map[string]jailedpayload.File{}))
	assert.NotEqual(t, hash, jailedpayload.Hash(map[string]jailedpayload.File{
		"/etc/slurm/topology.yaml": {Data: []byte("topology: []\n"), Mode: 0o600},
	}))
	assert.NotEqual(t, hash, jailedpayload.Hash(map[string]jailedpayload.File{
		"/etc/slurm/topology.conf": {Data: []byte("topology: []\n"), Mode: 0o644},
	}))
}

func Test_Hash_PathOrderIndependent(t *testing.T) {
	a := map[string]jailedpayload.File{"/a": {Data: []byte("a")}, "/b": {Data: []byte("b")}}
	b := map[string]jailedpayload.File{"/b": {Data: []byte("b")}, "/a": {Data: []byte("a")}}
	assert.Equal(t, jailedpayload.Hash(a), jailedpayload.Hash(b))
}
