/*
Copyright 2024 Nebius B.V.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package sconfigcontroller

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	k8srest "k8s.io/client-go/rest"

	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"nebius.ai/slurm-operator/internal/consts"
	fakefilestore "nebius.ai/slurm-operator/internal/controller/sconfigcontroller/fake"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

func newTestController(t *testing.T, configMap *corev1.ConfigMap) (*ControllerReconciler, *slurmapifake.MockClient, *fakefilestore.MockStore, error) {
	scheme := runtime.NewScheme()
	err := corev1.AddToScheme(scheme)
	if err != nil {
		return nil, nil, nil, err
	}

	mgr, err := ctrl.NewManager(&k8srest.Config{}, ctrl.Options{
		Scheme: scheme,
		NewClient: func(config *rest.Config, options client.Options) (client.Client, error) {
			return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(
				[]runtime.Object{configMap}...).Build(), nil
		},
	})
	if err != nil {
		return nil, nil, nil, err
	}

	apiClient := slurmapifake.NewMockClient(t)
	fakeStore := fakefilestore.MockStore{}

	sctrl := NewController(
		mgr.GetClient(),
		mgr.GetScheme(),
		mgr.GetEventRecorderFor(SConfigControllerName),
		apiClient,
		&fakeStore,
	)

	return sctrl, apiClient, &fakeStore, nil
}

func newBasicConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "soperator-slurm-configs",
			Namespace: "soperator",
			Labels: map[string]string{
				"slurm.nebius.ai/slurm-config": "general",
			},
		},
		Data: map[string]string{
			"config.conf": "config.conf content",
		},
	}
}

func TestController_SuccessFlow(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		request   ctrl.Request
		configMap corev1.ConfigMap
	}{
		{
			name: "flow with one config",
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "soperator-slurm-configs",
					Namespace: "soperator",
				},
			},
			configMap: corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "soperator-slurm-configs",
					Namespace: "soperator",
					Labels: map[string]string{
						consts.LabelSConfigControllerSourceKey: "true",
					},
				},
				Data: map[string]string{
					"config.conf": "config.conf content",
				},
			},
		},
		{
			name: "flow with many configs",
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "soperator-slurm-configs",
					Namespace: "soperator",
				},
			},
			configMap: corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "soperator-slurm-configs",
					Namespace: "soperator",
					Labels: map[string]string{
						consts.LabelSConfigControllerSourceKey: "true",
					},
				},
				Data: map[string]string{
					"config.conf":  "config.conf content",
					"config2.conf": "config2.conf content",
					"config3.conf": "config3.conf content",
				},
			},
		},
		{
			name: "flow with no configs",
			request: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "soperator-slurm-configs",
					Namespace: "soperator",
				},
			},
			configMap: corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "soperator-slurm-configs",
					Namespace: "soperator",
					Labels: map[string]string{
						consts.LabelSConfigControllerSourceKey: "true",
					},
				},
				Data: map[string]string{},
			},
		},
	}

	for _, tCase := range testCases {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()

			sctrl, apiClient, fakeStore, err := newTestController(t, &tCase.configMap)
			require.NoError(t, err)

			for configName, configContent := range tCase.configMap.Data {
				fakeStore.On("Add", configName, configContent, "").Once().Return(nil)
			}

			if len(tCase.configMap.Data) > 0 {
				apiClient.On("SlurmV0041GetReconfigureWithResponse", context.Background()).Once().Return(nil, nil)
			}

			_, err = sctrl.Reconcile(context.Background(), tCase.request)
			require.NoError(t, err)
		})
	}
}

func TestValidatePath(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		expectError bool
		errorSubstr string
	}{
		//  Valid paths
		{
			name:        "valid slurm path",
			path:        "/slurm/config.conf",
			expectError: false,
		},
		{
			name:        "valid slurm root path",
			path:        "/slurm",
			expectError: false,
		},
		{
			name:        "valid slurm nested path",
			path:        "/slurm/subdir/file.txt",
			expectError: false,
		},
		{
			name:        "valid slurm path with dots in filename",
			path:        "/slurm/config.file.conf",
			expectError: false,
		},

		// Wrong paths
		{
			name:        "empty path",
			path:        "",
			expectError: false,
		},
		{
			name:        "path without slurm prefix",
			path:        "/etc/config",
			expectError: true,
			errorSubstr: "must start with '/slurm'",
		},
		{
			name:        "relative path",
			path:        "slurm/config",
			expectError: true,
			errorSubstr: "must start with '/slurm'",
		},
		{
			name:        "path with slurm but not as prefix",
			path:        "/etc/slurm/config",
			expectError: true,
			errorSubstr: "must start with '/slurm'",
		},

		// Wrong paths with path traversal
		{
			name:        "path traversal with /..",
			path:        "/slurm/../etc/passwd",
			expectError: true,
			errorSubstr: "path traversal detected",
		},

		{
			name:        "path traversal in middle",
			path:        "/slurm/config/../../../etc/passwd",
			expectError: true,
			errorSubstr: "path traversal detected",
		},
		{
			name:        "path traversal at end",
			path:        "/slurm/config/..",
			expectError: true,
			errorSubstr: "path traversal detected",
		},
		{
			name:        "multiple path traversal",
			path:        "/slurm/../../../etc/passwd",
			expectError: true,
			errorSubstr: "path traversal detected",
		},

		// Edge cases
		{
			name:        "path with /.. in filename (should be blocked)",
			path:        "/slurm/file/..hidden",
			expectError: true,
			errorSubstr: "path traversal detected",
		},
		{
			name:        "path traversal at start",
			path:        "../slurm/config",
			expectError: true,
			errorSubstr: "must start with '/slurm'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePath(tt.path)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error for path %q, but got nil", tt.path)
					return
				}

				if !strings.Contains(err.Error(), tt.errorSubstr) {
					t.Errorf("expected error to contain %q, but got %q", tt.errorSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error for path %q, but got: %v", tt.path, err)
				}
			}
		})
	}
}

func TestTrimSlurmPrefixAlt(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "exact /slurm path",
			input:    "/slurm",
			expected: "",
		},
		{
			name:     "slurm with single subdirectory",
			input:    "/slurm/bastch",
			expected: "/bastch",
		},
		{
			name:     "slurm with nested path",
			input:    "/slurm/bastch/lom",
			expected: "/bastch/lom",
		},
		{
			name:     "slurm with deep nesting",
			input:    "/slurm/config/subdir/file.conf",
			expected: "/config/subdir/file.conf",
		},

		{
			name:     "slurm with trailing slash only",
			input:    "/slurm/",
			expected: "/",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},

		{
			name:     "path without slurm prefix",
			input:    "/etc/config",
			expected: "/etc/config",
		},
		{
			name:     "relative path with slurm",
			input:    "slurm/config",
			expected: "slurm/config",
		},
		{
			name:     "path with slurm in middle",
			input:    "/etc/slurm/config",
			expected: "/etc/slurm/config",
		},

		{
			name:     "slurm without leading slash",
			input:    "slurm",
			expected: "slurm",
		},
		{
			name:     "path containing slurm but not as prefix",
			input:    "/myslurmconfig",
			expected: "/myslurmconfig",
		},
		{
			name:     "multiple slurm occurrences",
			input:    "/slurm/slurm/config",
			expected: "/slurm/config",
		},

		{
			name:     "slurm with unicode path",
			input:    "/slurm/файл.conf",
			expected: "/файл.conf",
		},
		{
			name:     "slurm with special characters",
			input:    "/slurm/file@#$%.conf",
			expected: "/file@#$%.conf",
		},
		{
			name:     "slurm with spaces",
			input:    "/slurm/file with spaces.txt",
			expected: "/file with spaces.txt",
		},
		{
			name:     "very long path after slurm",
			input:    "/slurm/" + strings.Repeat("a", 1000),
			expected: "/" + strings.Repeat("a", 1000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := trimSlurmPrefix(tt.input)
			if result != tt.expected {
				t.Errorf("trimSlurmPrefixAlt(%q) = %q, expected %q",
					tt.input, result, tt.expected)
			}
		})
	}
}
