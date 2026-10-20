package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOverrideTestValuesNFS(t *testing.T) {
	for _, tc := range []struct {
		name     string
		version  string
		unstable bool
		enabled  bool
	}{
		{name: "candidate", version: "1.2.2-r2012dad5", unstable: true, enabled: true},
		{name: "stable", version: "1.2.2", enabled: true},
		{name: "disabled", version: "1.2.2-r2012dad5", unstable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nfs := map[string]interface{}{
				"enabled": tc.enabled, "version": "1.2.0", "use_stable_repo": true,
				"size_gibibytes": 3720, "threads": 32, "disk_type": "NETWORK_SSD_IO_M3",
			}
			result := overrideTestValues(map[string]interface{}{"nfs_in_k8s": nfs}, Config{
				SoperatorVersion: "4.1.12-r2012dad5", SoperatorUnstable: tc.unstable, NFSVersion: tc.version,
			})
			assert.Equal(t, map[string]interface{}{
				"enabled": tc.enabled, "version": tc.version, "use_stable_repo": !tc.unstable,
				"size_gibibytes": 3720, "threads": 32, "disk_type": "NETWORK_SSD_IO_M3",
			}, result["nfs_in_k8s"])
		})
	}
}

func TestOverrideTestValuesWithoutNFS(t *testing.T) {
	result := overrideTestValues(map[string]interface{}{}, Config{NFSVersion: "1.2.2"})
	assert.NotContains(t, result, "nfs_in_k8s")
	result = overrideTestValues(map[string]interface{}{"nfs_in_k8s": nil}, Config{NFSVersion: "1.2.2"})
	assert.Nil(t, result["nfs_in_k8s"])
}
