// Package jailedpayload renders the files a JailedConfig writes into the jail and fingerprints them.
//
// The hash is a contract between controllers: sconfigcontroller stores it in status.appliedHash
// after writing the files, and a controller that publishes a ConfigMap through a JailedConfig can
// compute the same hash for its own content to tell whether that content has been applied.
package jailedpayload

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"

	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/api/v1alpha1"
)

// File is one file of the payload: its contents and permission bits.
type File struct {
	Data []byte
	Mode int32
}

// Make renders the payload of a JailedConfig, keyed by absolute path inside the jail. It mirrors
// how Kubernetes projects a ConfigMap volume: with no items every key becomes a file under its own
// name, otherwise only the listed keys are rendered at their paths. An error means the spec is
// invalid, not that the ConfigMap is temporarily out of sync.
// See https://github.com/kubernetes/kubernetes/blob/b266ac2c3e42c2c4843f81e20213d2b2f43e450a/pkg/volume/configmap/configmap.go
func Make(spec *v1alpha1.JailedConfigSpec, configMap *corev1.ConfigMap) (map[string]File, error) {
	defaultMode := v1alpha1.DefaultMode
	if spec.DefaultMode != nil {
		defaultMode = *spec.DefaultMode
	}

	payload := make(map[string]File, len(configMap.Data)+len(configMap.BinaryData))
	if len(spec.Items) == 0 {
		for name, data := range configMap.Data {
			payload[name] = File{Data: []byte(data), Mode: defaultMode}
		}
		for name, data := range configMap.BinaryData {
			payload[name] = File{Data: data, Mode: defaultMode}
		}
	} else {
		for _, item := range spec.Items {
			var file File
			if stringData, ok := configMap.Data[item.Key]; ok {
				file.Data = []byte(stringData)
			} else if binaryData, ok := configMap.BinaryData[item.Key]; ok {
				file.Data = binaryData
			} else {
				return nil, fmt.Errorf("JailedConfig items references non-existent config key: %s", item.Key)
			}
			file.Mode = defaultMode
			if item.Mode != nil {
				file.Mode = *item.Mode
			}
			payload[item.Path] = file
		}
	}

	// Relative paths are a spec issue; path traversals, symlinks and permissions are left to the writer
	for path := range payload {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("invalid config path %q: must be absolute", path)
		}
	}
	return payload, nil
}

// Hash fingerprints what would be written to the jail for a JailedConfig. It covers the rendered
// payload rather than the ConfigMap alone, so changes to spec.Items or defaultMode count as changes
// too.
func Hash(payload map[string]File) string {
	paths := make([]string, 0, len(payload))
	for path := range payload {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	hash := sha256.New()
	for _, path := range paths {
		fmt.Fprintf(hash, "%s\x00%d\x00%d\x00", path, payload[path].Mode, len(payload[path].Data))
		hash.Write(payload[path].Data)
	}
	return hex.EncodeToString(hash.Sum(nil))
}
