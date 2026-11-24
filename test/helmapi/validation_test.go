//go:build helmapi

package helmapi

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

type manifest struct {
	source string
	object *unstructured.Unstructured
}

func TestFluxChartsApply(t *testing.T) {
	testEnv := &envtest.Environment{
		CRDs:               fluxCRDs(t),
		UseExistingCluster: ptr.To(false),
	}
	config, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testEnv.Stop()) })

	kube, err := client.New(config, client.Options{})
	require.NoError(t, err)
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	require.NoError(t, err)
	server, err := discoveryClient.ServerVersion()
	require.NoError(t, err)
	t.Logf("Kubernetes API server %s", server.GitVersion)

	profiles := []struct {
		name       string
		namespace  string
		valuesFile string
	}{
		{name: "default", namespace: "flux-system"},
		{name: "all-enabled", namespace: "flux-all-enabled", valuesFile: "testdata/all-enabled.yaml"},
	}
	for _, profile := range profiles {
		t.Run(profile.name, func(t *testing.T) {
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: profile.namespace}}
			require.NoError(t, kube.Create(t.Context(), namespace))

			bootstrap := renderChart(t, "soperator-fluxcd-bootstrap", profile.namespace, server.GitVersion,
				"--set-string", "helmRelease.namespace="+profile.namespace,
				"--set-string", "helmRepository.namespace="+profile.namespace)
			verifyManifests(t, kube, bootstrap, profile.namespace)

			var flags []string
			if profile.valuesFile != "" {
				flags = append(flags, "--values", profile.valuesFile)
			}
			umbrella := renderChart(t, "soperator-fluxcd", profile.namespace, server.GitVersion, flags...)
			verifyManifests(t, kube, umbrella, profile.namespace)

			namespaces := rawChartManifests(t, umbrella, "helm-soperator-fluxcd/templates/namespaces.yaml")
			require.NotEmpty(t, namespaces)
			seen := make(map[string]struct{}, len(namespaces))
			for _, namespace := range namespaces {
				name := namespace.object.GetName()
				require.NotContains(t, seen, name, "duplicate namespace declaration")
				seen[name] = struct{}{}
			}
			verifyManifests(t, kube, namespaces, profile.namespace)
			tailscale := rawChartManifests(t, umbrella, "helm-soperator-fluxcd/templates/tailscale.yaml")
			if profile.name == "all-enabled" {
				require.Len(t, tailscale, 2)
			}
			verifyManifests(t, kube, tailscale, profile.namespace)
		})
	}

	t.Run("rejects-schema-invalid-helmrelease", func(t *testing.T) {
		invalid := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "helm.toolkit.fluxcd.io/v2",
			"kind":       "HelmRelease",
			"metadata":   map[string]any{"name": "invalid-interval", "namespace": "default"},
			"spec": map[string]any{
				"interval": "invalid",
				"chartRef": map[string]any{"kind": "OCIRepository", "name": "example"},
			},
		}}
		_, err := applyManifest(t, kube, invalid)
		require.True(t, apierrors.IsInvalid(err), "expected CRD validation error, got %v", err)
		require.ErrorContains(t, err, "spec.interval")
	})
}

func TestDecodeManifestsRejectsDuplicateKeys(t *testing.T) {
	_, err := decodeManifests([]byte(`
# Source: example/templates/helmrelease.yaml
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: example
spec:
  values:
    clusterName: first
    clusterName: second
`))
	require.ErrorContains(t, err, "example/templates/helmrelease.yaml")
	require.ErrorContains(t, err, "key \"clusterName\" already set")
}

func fluxCRDs(t *testing.T) []*apiextensionsv1.CustomResourceDefinition {
	t.Helper()
	flux := os.Getenv("FLUX")
	require.NotEmpty(t, flux, "run this test with make test-helm-api")
	manifests, err := decodeManifests(commandOutput(t, flux, "install", "--export"))
	require.NoError(t, err)

	var crds []*apiextensionsv1.CustomResourceDefinition
	for _, manifest := range manifests {
		if manifest.object.GetKind() != "CustomResourceDefinition" {
			continue
		}
		switch manifest.object.GetName() {
		case "helmreleases.helm.toolkit.fluxcd.io", "helmrepositories.source.toolkit.fluxcd.io":
			crd := &apiextensionsv1.CustomResourceDefinition{}
			require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(manifest.object.Object, crd), manifest.object.GetName())
			crds = append(crds, crd)
		}
	}
	require.Len(t, crds, 2, "Flux export must provide the HelmRelease and HelmRepository CRDs")
	return crds
}

func renderChart(t *testing.T, chart, namespace, kubeVersion string, flags ...string) []manifest {
	t.Helper()
	args := []string{
		"template", chart, filepath.Join("..", "..", "helm", chart),
		"--namespace", namespace, "--kube-version", kubeVersion,
	}
	manifests, err := decodeManifests(commandOutput(t, "helm", append(args, flags...)...))
	require.NoError(t, err, "decode %s manifests", chart)
	require.NotEmpty(t, manifests, "%s must render manifests", chart)
	return manifests
}

func commandOutput(t *testing.T, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	require.NoErrorf(t, err, "%s %v: %s", name, args, stderr.String())
	return output
}

func decodeManifests(output []byte) ([]manifest, error) {
	reader := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(output)))
	var manifests []manifest
	for document := 1; ; document++ {
		data, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return manifests, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read document %d: %w", document, err)
		}

		source := fmt.Sprintf("document %d", document)
		for line := range bytes.SplitSeq(data, []byte("\n")) {
			if path, ok := bytes.CutPrefix(line, []byte("# Source: ")); ok {
				source = string(bytes.TrimSpace(path))
				break
			}
		}

		var fields map[string]any
		if err := yaml.UnmarshalStrict(data, &fields); err != nil {
			return nil, fmt.Errorf("decode %s: %w", source, err)
		}
		if fields != nil {
			manifests = append(manifests, manifest{source: source, object: &unstructured.Unstructured{Object: fields}})
		}
	}
}

func rawChartManifests(t *testing.T, releases []manifest, template string) []manifest {
	t.Helper()
	var manifests []manifest
	for _, release := range releases {
		if release.source != template {
			continue
		}
		resources, _, err := unstructured.NestedSlice(release.object.Object, "spec", "values", "resources")
		require.NoError(t, err, template)
		for _, resource := range resources {
			fields, ok := resource.(map[string]any)
			require.True(t, ok, "%s must pass objects to the Raw chart", template)
			manifests = append(manifests, manifest{source: template, object: &unstructured.Unstructured{Object: fields}})
		}
	}
	return manifests
}

func verifyManifests(t *testing.T, kube client.Client, manifests []manifest, namespace string) {
	t.Helper()
	for _, manifest := range manifests {
		object := manifest.object
		description := fmt.Sprintf("%s (%s %s)", manifest.source, object.GroupVersionKind(), object.GetName())
		namespaced, err := kube.IsObjectNamespaced(object)
		require.NoError(t, err, description)
		if namespaced && object.GetNamespace() == "" {
			object.SetNamespace(namespace)
		}

		applied, err := applyManifest(t, kube, object)
		require.NoErrorf(t, err, "apply %s", description)
		require.NotEmpty(t, applied.GetUID(), description)
		reapplied, err := applyManifest(t, kube, object)
		require.NoErrorf(t, err, "reapply %s", description)
		require.Equalf(t, applied.GetUID(), reapplied.GetUID(), "reapply %s must keep its UID", description)

		stored := &unstructured.Unstructured{}
		stored.SetGroupVersionKind(object.GroupVersionKind())
		err = kube.Get(t.Context(), client.ObjectKeyFromObject(object), stored)
		require.NoErrorf(t, err, "read back %s", description)
		require.Equal(t, applied.GetUID(), stored.GetUID(), description)
	}
	t.Logf("Applied, reapplied, and read back %d manifests in %s", len(manifests), namespace)
}

func applyManifest(t *testing.T, kube client.Client, object *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	t.Helper()
	data, err := object.MarshalJSON()
	require.NoError(t, err)
	applied := object.DeepCopy()
	err = kube.Patch(t.Context(), applied, client.RawPatch(types.ApplyPatchType, data),
		client.FieldOwner("helm-api-validation"), client.FieldValidation(metav1.FieldValidationStrict))
	return applied, err
}
