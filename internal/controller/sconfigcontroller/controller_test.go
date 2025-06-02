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
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	k8srest "k8s.io/client-go/rest"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
						"slurm.nebius.ai/slurm-config": "general",
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
						"slurm.nebius.ai/slurm-config": "general",
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
						"slurm.nebius.ai/slurm-config": "general",
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
				fakeStore.On("Add", configName, configContent).Once().Return(nil)
			}

			if len(tCase.configMap.Data) > 0 {
				apiClient.On("SlurmV0041GetReconfigureWithResponse", context.Background()).Once().Return(nil, nil)
			}

			_, err = sctrl.Reconcile(context.Background(), tCase.request)
			require.NoError(t, err)
		})
	}
}

func TestController_FailFlow_ClientGetError(t *testing.T) {
	t.Parallel()

	sctrl, apiClient, fakeStore, err := newTestController(t, newBasicConfigMap())
	require.NoError(t, err)

	fakeStore.AssertNotCalled(t, "Add", mock.AnythingOfType("string"), mock.AnythingOfType("string"))
	apiClient.AssertNotCalled(t, "SlurmV0041GetReconfigureWithResponse", context.Background())

	_, err = sctrl.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "somename",
			Namespace: "somenamespace",
		},
	})
	require.Error(t, err)
}

func TestController_FailFlow_StoreAddError(t *testing.T) {
	t.Parallel()

	sctrl, apiClient, fakeStore, err := newTestController(t, newBasicConfigMap())
	require.NoError(t, err)

	fakeStore.On("Add", "config.conf", "config.conf content").Once().Return(errors.New("adding file error"))
	apiClient.AssertNotCalled(t, "SlurmV0041GetReconfigureWithResponse", context.Background())

	_, err = sctrl.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "soperator-slurm-configs",
			Namespace: "soperator",
		},
	})
	require.Error(t, err)
}

func TestController_FailFlow_SlurmAPIReconfigureError(t *testing.T) {
	t.Parallel()

	sctrl, apiClient, fakeStore, err := newTestController(t, newBasicConfigMap())
	require.NoError(t, err)

	fakeStore.On("Add", mock.AnythingOfType("string"), mock.AnythingOfType("string")).Once().Return(nil)
	apiClient.On("SlurmV0041GetReconfigureWithResponse", context.Background()).Once().Return(nil, errors.New("reconfiguring slurm cluster error"))

	_, err = sctrl.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "soperator-slurm-configs",
			Namespace: "soperator",
		},
	})
	require.Error(t, err)
}
