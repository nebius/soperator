package controllerconfig

import (
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestIgnoredSecretTypesFromEnv(t *testing.T) {
	tests := []struct {
		name  string
		value string
		unset bool
		want  []string
	}{
		{name: "unset disables filtering", unset: true},
		{name: "disabled", value: ""},
		{name: "empty entries", value: " , \t, "},
		{name: "custom type", value: "example.com/archive", want: []string{"example.com/archive"}},
		{name: "multiple types", value: "helm.sh/release.v1,example.com/archive", want: []string{"helm.sh/release.v1", "example.com/archive"}},
		{name: "whitespace and duplicates", value: " helm.sh/release.v1 , ,example.com/archive, helm.sh/release.v1, ", want: []string{"helm.sh/release.v1", "example.com/archive"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(IgnoredSecretTypesEnv, tt.value)
			if tt.unset {
				require.NoError(t, os.Unsetenv(IgnoredSecretTypesEnv))
			}
			require.Equal(t, tt.want, IgnoredSecretTypesFromEnv())
		})
	}
}

func TestCacheByObject(t *testing.T) {
	tests := []struct {
		name    string
		ignored []string
		allowed []string
	}{
		{name: "disabled", allowed: []string{"", "Opaque", "kubernetes.io/tls", "helm.sh/release.v1"}},
		{name: "helm", ignored: []string{"helm.sh/release.v1"}, allowed: []string{"", "Opaque", "kubernetes.io/tls", "example.com/archive"}},
		{name: "multiple", ignored: []string{"helm.sh/release.v1", "example.com/archive"}, allowed: []string{"", "Opaque", "kubernetes.io/tls"}},
		{name: "custom replaces helm", ignored: []string{"example.com/archive"}, allowed: []string{"helm.sh/release.v1", "Opaque"}},
		{name: "escaped values", ignored: []string{"example.com/a,b=c"}, allowed: []string{"example.com/a", "helm.sh/release.v1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			byObject := CacheByObject(tt.ignored, logr.Discard())
			nodeConfig, ok := cacheConfigFor(byObject, &corev1.Node{})
			require.True(t, ok)
			require.NotNil(t, nodeConfig.Transform)
			node := &corev1.Node{Status: corev1.NodeStatus{Images: []corev1.ContainerImage{{Names: []string{"image"}}}}}
			_, err := nodeConfig.Transform(node)
			require.NoError(t, err)
			require.Empty(t, node.Status.Images)

			secretConfig, ok := cacheConfigFor(byObject, &corev1.Secret{})
			if len(tt.ignored) == 0 {
				require.False(t, ok)
				return
			}
			require.True(t, ok)
			require.Nil(t, secretConfig.Namespaces, "inherit the manager's namespace restrictions")
			require.Nil(t, secretConfig.Transform, "preserve Secret data")
			selector, err := fields.ParseSelector(secretConfig.Field.String())
			require.NoError(t, err)
			for _, secretType := range tt.ignored {
				require.False(t, selector.Matches(fields.Set{"type": secretType}), secretType)
			}
			for _, secretType := range tt.allowed {
				require.True(t, selector.Matches(fields.Set{"type": secretType}), secretType)
			}
		})
	}
}

func TestCacheByObjectLogsAtDebugLevel(t *testing.T) {
	for _, level := range []zapcore.Level{zapcore.InfoLevel, zapcore.DebugLevel} {
		for _, enabled := range []bool{false, true} {
			t.Run(level.String()+"/filtering="+strconv.FormatBool(enabled), func(t *testing.T) {
				core, logs := observer.New(level)
				var ignored []string
				if enabled {
					ignored = []string{"helm.sh/release.v1"}
				}
				CacheByObject(ignored, zapr.NewLogger(zap.New(core)))
				if level == zapcore.InfoLevel {
					require.Zero(t, logs.Len())
					return
				}
				require.Equal(t, 1, logs.Len())
				entry := logs.All()[0]
				require.Equal(t, zapcore.DebugLevel, entry.Level)
				if enabled {
					require.Equal(t, "Skipping Secret types in cache LIST/WATCH requests", entry.Message)
					require.Equal(t, []any{"helm.sh/release.v1"}, entry.ContextMap()["ignoredSecretTypes"])
					require.Equal(t, "type!=helm.sh/release.v1", entry.ContextMap()["fieldSelector"])
				} else {
					require.Equal(t, "Secret type filtering is disabled", entry.Message)
				}
			})
		}
	}
}

func cacheConfigFor(byObject map[client.Object]cache.ByObject, obj client.Object) (cache.ByObject, bool) {
	for key, config := range byObject {
		if reflect.TypeOf(key) == reflect.TypeOf(obj) {
			return config, true
		}
	}
	return cache.ByObject{}, false
}

func TestTrimNodeForCache(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:          "worker-0",
			Labels:        map[string]string{"topology.nebius.com/tier-1": "switch-1"},
			ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "kubelet"}},
		},
		Spec: corev1.NodeSpec{Unschedulable: true},
		Status: corev1.NodeStatus{
			Images:     []corev1.ContainerImage{{Names: []string{"cr.nebius.cloud/soperator:1.0.0"}}},
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
		},
	}

	trimmed, err := TrimNodeForCache(node)
	require.NoError(t, err)

	trimmedNode, ok := trimmed.(*corev1.Node)
	require.True(t, ok)
	require.Empty(t, trimmedNode.ManagedFields)
	require.Empty(t, trimmedNode.Status.Images)

	// Everything the controllers actually read must survive.
	require.Equal(t, "worker-0", trimmedNode.Name)
	require.Equal(t, "switch-1", trimmedNode.Labels["topology.nebius.com/tier-1"])
	require.True(t, trimmedNode.Spec.Unschedulable)
	require.Equal(t, corev1.NodeReady, trimmedNode.Status.Conditions[0].Type)
}

func TestTrimNodeForCache_passesThroughNonNodes(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker-0"}}

	trimmed, err := TrimNodeForCache(pod)
	require.NoError(t, err)
	require.Same(t, pod, trimmed)
}
