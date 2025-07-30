/*
Copyright 2025 Nebius B.V.

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
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	k8srest "k8s.io/client-go/rest"
	"k8s.io/utils/ptr"

	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v0041 "github.com/SlinkyProject/slurm-client/api/v0041"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	fakes "nebius.ai/slurm-operator/internal/controller/sconfigcontroller/fake"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

func newTestJailedConfigController(
	t *testing.T,
	configMap *corev1.ConfigMap,
	jailedConfig *slurmv1alpha1.JailedConfig,
) (*JailedConfigReconciler, *slurmapifake.MockClient, *fakes.MockFs, *fakes.MockClock, error) {
	scheme := runtime.NewScheme()
	err := corev1.AddToScheme(scheme)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	err = slurmv1alpha1.AddToScheme(scheme)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(jailedConfig).
		WithRuntimeObjects(
			configMap,
			jailedConfig,
		).Build()

	mgr, err := ctrl.NewManager(&k8srest.Config{}, ctrl.Options{
		Scheme: scheme,
		NewClient: func(config *rest.Config, options client.Options) (client.Client, error) {
			return fakeClient, nil
		},
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}

	clock := fakes.NewMockClock(t)
	apiClient := slurmapifake.NewMockClient(t)
	fakeFs := fakes.NewMockFs(t)

	sctrl := NewJailedConfigReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		apiClient,
		fakeFs,
	)

	sctrl.clock = clock

	return sctrl, apiClient, fakeFs, clock, nil
}

const (
	testNamespace    = "soperator"
	testConfigMap    = "soperator-slurm-configs"
	testJailedConfig = "soperator-jailed-config"
)

type testOptions struct {
	configMap    corev1.ConfigMap
	jailedConfig slurmv1alpha1.JailedConfig
}

type testOption func(*testOptions)

func withConfigMapData(data map[string]string) testOption {
	return func(args *testOptions) {
		args.configMap.Data = data
	}
}

func withConfigMapBinaryData(data map[string][]byte) testOption {
	return func(args *testOptions) {
		args.configMap.BinaryData = data
	}
}

func withItems(items []corev1.KeyToPath) testOption {
	return func(args *testOptions) {
		args.jailedConfig.Spec.Items = items
	}
}

func withDefaultMode(defaultMode *int32) testOption {
	return func(args *testOptions) {
		args.jailedConfig.Spec.DefaultMode = defaultMode
	}
}

func withUpdateActions(actions []slurmv1alpha1.UpdateAction) testOption {
	return func(args *testOptions) {
		args.jailedConfig.Spec.UpdateActions = actions
	}
}

func withMissingConfigMap() testOption {
	return func(args *testOptions) {
		args.jailedConfig.Spec.ConfigMap.Name = ""
	}
}

func prepareTest(t *testing.T, options ...testOption) (*JailedConfigReconciler, ctrl.Request, *slurmapifake.MockClient, *fakes.MockFs, *fakes.MockClock) {
	opts := &testOptions{
		configMap: corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      testConfigMap,
				Namespace: testNamespace,
			},
		},
		jailedConfig: slurmv1alpha1.JailedConfig{
			TypeMeta: metav1.TypeMeta{
				Kind:       "JailedConfig",
				APIVersion: "slurm.nebius.ai/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      testJailedConfig,
				Namespace: testNamespace,
			},
			Spec: slurmv1alpha1.JailedConfigSpec{
				ConfigMap: slurmv1alpha1.ConfigMapReference{
					Name: testConfigMap,
				},
			},
			Status: slurmv1alpha1.JailedConfigStatus{},
		},
	}

	for _, option := range options {
		option(opts)
	}

	request := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      testJailedConfig,
			Namespace: testNamespace,
		},
	}

	sctrl, apiClient, fakeFs, clock, err := newTestJailedConfigController(t, &opts.configMap, &opts.jailedConfig)
	require.NoError(t, err)

	return sctrl, request, apiClient, fakeFs, clock
}

func prepareFs(
	fs *fakes.MockFs,
	dirName string,
	fileName string,
	content []byte,
	fileMode os.FileMode,
) {
	tempFileName := fileName + ".tmp"

	mock.InOrder(
		fs.
			On("MkdirAll", dirName, os.FileMode(0o755)).
			Return(nil),
		fs.
			On("PrepareNewFile", fileName, content, fileMode).
			Return(tempFileName, nil),
		fs.
			On("RenameExchange", tempFileName, fileName).
			Return(nil),
		fs.
			On("SyncCaches").
			Return(nil),
		fs.
			On("Remove", tempFileName).
			Return(nil),
	)
}

func prepareSlurmApi(
	slurmapi *slurmapifake.MockClient,
	clock *fakes.MockClock,
) {
	nodeNames := []string{"node1", "node2", "node3"}
	slurmdStartTimeBefore := int64(1)
	slurmdStartTimeAfter := int64(2)

	var nodesBefore v0041.V0041Nodes
	for _, nodeName := range nodeNames {
		nodesBefore = append(nodesBefore, v0041.V0041Node{
			Name: &nodeName,
			SlurmdStartTime: &v0041.V0041Uint64NoValStruct{
				Infinite: ptr.To(false),
				Number:   &slurmdStartTimeBefore,
				Set:      ptr.To(true),
			},
		})
	}

	var nodesOneDone v0041.V0041Nodes
	for i, nodeName := range nodeNames {
		slurmdStartTime := slurmdStartTimeBefore
		if i == 0 {
			slurmdStartTime = slurmdStartTimeAfter
		}

		nodesOneDone = append(nodesOneDone, v0041.V0041Node{
			Name: &nodeName,
			SlurmdStartTime: &v0041.V0041Uint64NoValStruct{
				Infinite: ptr.To(false),
				Number:   &slurmdStartTime,
				Set:      ptr.To(true),
			},
		})
	}

	var nodesAfter v0041.V0041Nodes
	for _, nodeName := range nodeNames {
		nodesAfter = append(nodesAfter, v0041.V0041Node{
			Name: &nodeName,
			SlurmdStartTime: &v0041.V0041Uint64NoValStruct{
				Infinite: ptr.To(false),
				Number:   &slurmdStartTimeAfter,
				Set:      ptr.To(true),
			},
		})
	}

	mkNodes200Resp := func(nodes v0041.V0041Nodes) *v0041.SlurmV0041GetNodesResponse {
		return &v0041.SlurmV0041GetNodesResponse{
			HTTPResponse: &http.Response{
				StatusCode: http.StatusOK,
			},
			JSON200: &v0041.V0041OpenapiNodesResp{
				Errors: &[]v0041.V0041OpenapiError{},
				Nodes:  nodes,
			},
		}
	}

	nodesBeforeResp := mkNodes200Resp(nodesBefore)

	reconfigureResponse := &v0041.SlurmV0041GetReconfigureResponse{
		HTTPResponse: &http.Response{
			StatusCode: http.StatusOK,
		},
		JSON200: &v0041.V0041OpenapiResp{
			Errors: &[]v0041.V0041OpenapiError{},
		},
	}

	nodesOneDoneResp := mkNodes200Resp(nodesOneDone)
	nodesAfterResp := mkNodes200Resp(nodesAfter)

	mkTimeChan := func() <-chan time.Time {
		now := time.Now()
		res := make(chan time.Time, 1)
		res <- now
		close(res)
		return res
	}

	mock.InOrder(
		slurmapi.
			On("SlurmV0041GetNodesWithResponse", anyContext, emptyGetNodesParams).
			Return(nodesBeforeResp, nil).
			Once(),
		slurmapi.
			On("SlurmV0041GetReconfigureWithResponse", anyContext).
			Return(reconfigureResponse, nil),
		slurmapi.
			On("SlurmV0041GetNodesWithResponse", anyContext, emptyGetNodesParams).
			Return(nodesBeforeResp, nil).
			Once(),
		clock.
			On("After", 1*time.Second).
			Return(mkTimeChan()).
			Once(),
		slurmapi.
			On("SlurmV0041GetNodesWithResponse", anyContext, emptyGetNodesParams).
			Return(nodesOneDoneResp, nil).
			Once(),
		clock.
			On("After", 1*time.Second).
			Return(mkTimeChan()).
			Once(),
		slurmapi.
			On("SlurmV0041GetNodesWithResponse", anyContext, emptyGetNodesParams).
			Return(nodesAfterResp, nil),
	)
}

var anyContext = mock.MatchedBy(func(val interface{}) bool {
	_, ok := val.(context.Context)
	return ok
})

var emptyGetNodesParams *v0041.SlurmV0041GetNodesParams = nil

func TestJailedConfigReconciler_Empty(t *testing.T) {
	sctrl, request, _, _, _ := prepareTest(t) //nolint:dogsled

	// Expect nothing to happen in fs and slurm API

	_, err := sctrl.Reconcile(context.Background(), request)
	require.NoError(t, err)
}

func TestJailedConfigReconciler_SingleData(t *testing.T) {
	t.Parallel()

	dirName := "/etc"
	fileName := "/etc/config.txt"
	content := "config data"

	sctrl, request, _, fs, _ := prepareTest(
		t,
		withConfigMapData(map[string]string{
			fileName: content,
		}),
	)

	prepareFs(fs, dirName, fileName, []byte(content), os.FileMode(0o644))

	_, err := sctrl.Reconcile(context.Background(), request)
	require.NoError(t, err)
}

func TestJailedConfigReconciler_SingleBinaryData(t *testing.T) {
	t.Parallel()

	dirName := "/etc"
	fileName := "/etc/config.txt"
	content := []byte{1, 2, 3}

	sctrl, request, _, fs, _ := prepareTest(
		t,
		withConfigMapBinaryData(map[string][]byte{
			fileName: content,
		}),
	)

	prepareFs(fs, dirName, fileName, content, os.FileMode(0o644))

	_, err := sctrl.Reconcile(context.Background(), request)
	require.NoError(t, err)

}

func TestJailedConfigReconciler_Mode(t *testing.T) {
	t.Parallel()

	dirName := "/etc"

	type testFile struct {
		fileName     string
		mode         *int32
		expectedMode os.FileMode
	}

	testCases := []struct {
		name        string
		defaultMode *int32
		files       []testFile
	}{
		{
			name:        "single file, no modes",
			defaultMode: nil,
			files: []testFile{{
				fileName:     "a",
				mode:         nil,
				expectedMode: 0o644,
			}},
		},
		{
			name:        "single file, defaultMode",
			defaultMode: ptr.To(int32(0o700)),
			files: []testFile{{
				fileName:     "a",
				mode:         nil,
				expectedMode: 0o700,
			}},
		},
		{
			name:        "single file, mode",
			defaultMode: nil,
			files: []testFile{{
				fileName:     "a",
				mode:         ptr.To(int32(0o700)),
				expectedMode: 0o700,
			}},
		},
		{
			name:        "two files, all modes different",
			defaultMode: ptr.To(int32(0o755)),
			files: []testFile{
				{
					fileName:     "a",
					mode:         ptr.To(int32(0o750)),
					expectedMode: 0o750,
				},
				{
					fileName:     "b",
					mode:         ptr.To(int32(0o740)),
					expectedMode: 0o740,
				},
			},
		},
	}

	for _, tCase := range testCases {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()

			content := []byte{1, 2, 3}

			configMapData := map[string][]byte{}
			for _, file := range tCase.files {
				configMapData[file.fileName] = content
			}

			var items []corev1.KeyToPath
			for _, file := range tCase.files {
				items = append(items, corev1.KeyToPath{
					Key:  file.fileName,
					Path: dirName + "/" + file.fileName,
					Mode: file.mode,
				})
			}

			sctrl, request, _, fs, _ := prepareTest(
				t,
				withConfigMapBinaryData(configMapData),
				withItems(items),
				withDefaultMode(tCase.defaultMode),
			)

			for _, file := range tCase.files {
				fileName := dirName + "/" + file.fileName
				prepareFs(
					fs,
					dirName,
					fileName,
					content,
					file.expectedMode,
				)
			}

			_, err := sctrl.Reconcile(context.Background(), request)
			require.NoError(t, err)
		})
	}
}

func TestJailedConfigReconciler_Path(t *testing.T) {
	t.Parallel()

	// this dirname must match all dirnames of absolute path test cases
	dirName := "/etc"
	absoluteFileName := "/etc/config.txt"
	relativeFileName := "foo/bar/config.txt"
	plainFileName := "config.txt"

	testCases := []struct {
		name             string
		configMapKey     string
		jailedConfigPath *string
		error            bool
	}{
		{
			name:             "absolute path in ConfigMap, no KeyToPath",
			configMapKey:     absoluteFileName,
			jailedConfigPath: nil,
			error:            false,
		},
		{
			name:             "relative path in ConfigMap, no KeyToPath",
			configMapKey:     relativeFileName,
			jailedConfigPath: nil,
			error:            true,
		},
		{
			name:             "plain file name in ConfigMap, absolute KeyToPath",
			configMapKey:     plainFileName,
			jailedConfigPath: &absoluteFileName,
			error:            false,
		},
		{
			name:             "plain file name in ConfigMap, relative KeyToPath",
			configMapKey:     plainFileName,
			jailedConfigPath: &relativeFileName,
			error:            true,
		},
	}

	for _, tCase := range testCases {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()

			content := []byte{1, 2, 3}

			var items []corev1.KeyToPath
			if tCase.jailedConfigPath != nil {
				items = append(items, corev1.KeyToPath{
					Key:  tCase.configMapKey,
					Path: *tCase.jailedConfigPath,
				})
			}

			sctrl, request, _, fs, _ := prepareTest(
				t,
				withConfigMapBinaryData(map[string][]byte{
					tCase.configMapKey: content,
				}),
				withItems(items),
			)

			if tCase.error {
				_, err := sctrl.Reconcile(context.Background(), request)
				require.ErrorContains(t, err, "must be absolute")
			} else {
				prepareFs(fs, dirName, absoluteFileName, content, os.FileMode(0o644))

				_, err := sctrl.Reconcile(context.Background(), request)
				require.NoError(t, err)
			}
		})
	}
}

func TestJailedConfigReconciler_Reconfigure(t *testing.T) {
	dirName := "/etc"
	fileName := "/etc/config.txt"
	content := "config data"

	sctrl, request, slurmapi, fs, clock := prepareTest(
		t,
		withConfigMapData(map[string]string{
			fileName: content,
		}),
		withUpdateActions([]slurmv1alpha1.UpdateAction{slurmv1alpha1.UpdateActionReconfigure}),
	)

	prepareFs(fs, dirName, fileName, []byte(content), os.FileMode(0o644))
	prepareSlurmApi(slurmapi, clock)

	_, err := sctrl.Reconcile(context.Background(), request)
	require.NoError(t, err)
}

func TestJailedConfigReconciler_MissingConfigMapInSpec(t *testing.T) {
	sctrl, request, _, _, _ := prepareTest( //nolint:dogsled
		t,
		withMissingConfigMap(),
	)

	_, err := sctrl.Reconcile(context.Background(), request)
	require.ErrorContains(t, err, "not found")
}

func TestJailedConfigReconciler_MissingConfigMapKeyInItems(t *testing.T) {
	sctrl, request, _, _, _ := prepareTest( //nolint:dogsled
		t,
		withConfigMapData(map[string]string{
			"foo": "",
		}),
		withItems([]corev1.KeyToPath{{
			Key:  "bar",
			Path: "/etc/config.txt",
		}}),
	)

	_, err := sctrl.Reconcile(context.Background(), request)
	require.ErrorContains(t, err, "references non-existent config key")
}
