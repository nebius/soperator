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
	"strings"
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

	maxFullname := strings.Repeat("a", 63)
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
				flags = append(flags, "--values", profile.valuesFile, "--set-string", "fullnameOverride="+maxFullname)
			}
			umbrella := renderChart(t, "soperator-fluxcd", profile.namespace, server.GitVersion, flags...)
			verifyManifests(t, kube, umbrella, profile.namespace)
			values := verifyBaseValues(t, kube, umbrella)

			namespaces := rawChartManifests(t, umbrella, values, "helm-soperator-fluxcd/templates/namespaces.yaml")
			require.NotEmpty(t, namespaces)
			seen := make(map[string]struct{}, len(namespaces))
			for _, namespace := range namespaces {
				name := namespace.object.GetName()
				require.NotContains(t, seen, name, "duplicate namespace declaration")
				seen[name] = struct{}{}
			}
			verifyManifests(t, kube, namespaces, profile.namespace)
			tailscale := rawChartManifests(t, umbrella, values, "helm-soperator-fluxcd/templates/tailscale.yaml")
			if profile.name == "all-enabled" {
				require.Len(t, tailscale, 2)
				verifyNotifierValuesPrecedence(t, testEnv, kube, profile.namespace, maxFullname+"-soperator-notifier")
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

func verifyBaseValues(t *testing.T, kube client.Client, manifests []manifest) map[string]map[string]any {
	t.Helper()
	seen := make(map[client.ObjectKey]string)
	values := make(map[string]map[string]any)
	for _, manifest := range manifests {
		release := manifest.object
		if release.GetKind() != "HelmRelease" {
			continue
		}
		name := release.GetName()
		_, inline, err := unstructured.NestedFieldNoCopy(release.Object, "spec", "values")
		require.NoError(t, err, name)
		require.False(t, inline, "%s must omit spec.values", name)
		refs, _, err := unstructured.NestedSlice(release.Object, "spec", "valuesFrom")
		require.NoError(t, err, name)
		require.NotEmpty(t, refs, name)
		base, ok := refs[0].(map[string]any)
		require.True(t, ok, name)
		require.Equal(t, "ConfigMap", base["kind"], name)
		require.Equal(t, "values.yaml", base["valuesKey"], name)
		require.NotEqual(t, true, base["optional"], name)
		require.NotContains(t, base, "targetPath", name)
		baseName := name + "-values"
		if prefix, notifier := strings.CutSuffix(name, "-soperator-notifier"); notifier {
			baseName = prefix + "-notifier-values"
		}
		require.Equal(t, baseName, base["name"], name)
		key := client.ObjectKey{Namespace: release.GetNamespace(), Name: baseName}
		require.NotContains(t, seen, key, "%s shares its base ConfigMap with %s", name, seen[key])
		seen[key] = name
		var cm corev1.ConfigMap
		require.NoError(t, kube.Get(t.Context(), key, &cm), name)
		require.Equal(t, "Enabled", cm.Labels["reconcile.fluxcd.io/watch"], key)
		var payload map[string]any
		require.NoError(t, yaml.UnmarshalStrict([]byte(cm.Data["values.yaml"]), &payload), key)
		require.NotNil(t, payload, "%s must contain a YAML mapping at data[values.yaml]", key)
		values[name] = payload
	}
	require.NotEmpty(t, values, "umbrella must render child HelmReleases")
	return values
}

func verifyNotifierValuesPrecedence(t *testing.T, testEnv *envtest.Environment, kube client.Client, namespace, name string) {
	t.Helper()
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "team-notifier", Namespace: namespace},
		Data:       map[string]string{"notifier.yaml": "nameOverride: team-notifier\nfullnameOverride: team-fullname"},
	}
	require.NoError(t, kube.Create(t.Context(), configMap))
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "site-notifier", Namespace: namespace},
		StringData: map[string]string{"notifier.yaml": "nameOverride: site-notifier"},
	}
	require.NoError(t, kube.Create(t.Context(), secret))

	user, err := testEnv.AddUser(envtest.User{Name: "flux-debug", Groups: []string{"system:masters"}}, nil)
	require.NoError(t, err)
	config, err := user.KubeConfig()
	require.NoError(t, err)
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfig, config, 0o600))
	output := commandOutput(t, os.Getenv("FLUX"), "debug", "hr", name,
		"--namespace", namespace, "--kubeconfig", kubeconfig, "--cache-dir", dir, "--show-values")
	var merged map[string]any
	require.NoError(t, yaml.UnmarshalStrict(output, &merged))
	require.Equal(t, map[string]any{"webhookUrl": "https://example.invalid/webhook"}, merged["slack"])
	require.Equal(t, "team-fullname", merged["fullnameOverride"])
	require.Equal(t, "site-notifier", merged["nameOverride"])
}

func rawChartManifests(t *testing.T, releases []manifest, values map[string]map[string]any, template string) []manifest {
	t.Helper()
	var manifests []manifest
	for _, release := range releases {
		if release.source != template || release.object.GetKind() != "HelmRelease" {
			continue
		}
		resources, _, err := unstructured.NestedSlice(values[release.object.GetName()], "resources")
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
