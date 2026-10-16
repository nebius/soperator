package sharedsteps

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const kruiseStatefulSetResource = "statefulsets.apps.kruise.io"

func findMetaCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func k8sNodeReady(node corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

const (
	checksSlurmNodesIntervalFlag = "--requeue-after-slurm-nodes"
	checksMaintenanceInterval    = 15 * time.Second
	checksDeploymentTimeout      = 5 * time.Minute
	helmDriftAnnotation          = "helm.toolkit.fluxcd.io/driftDetection"
	helmReleaseResource          = "helmreleases.helm.toolkit.fluxcd.io"
)

type helmReleaseList struct {
	Items []helmRelease `json:"items"`
}

type helmRelease struct {
	Metadata metav1.ObjectMeta `json:"metadata"`
	Spec     struct {
		ReleaseName     string `json:"releaseName"`
		TargetNamespace string `json:"targetNamespace"`
		Suspend         *bool  `json:"suspend,omitempty"`
	} `json:"spec"`
	Status helmReleaseStatusRef `json:"status"`
}

type helmReleaseStatusRef struct {
	Conditions []metav1.Condition `json:"conditions"`
}

type temporaryChecksInterval struct {
	deployment      appsv1.Deployment
	containerIndex  int
	args            []string
	release         *helmRelease
	deploymentOwned bool
	releaseOwned    bool
}

// TODO: Remove this temporary interval override, rollback, and associated tests
// once --requeue-after-slurm-nodes is configured in the Helm chart for E2E clusters.
//
// Return the saved state even after a partial mutation so cleanup can restore it.
func prepareMaintenanceChecksInterval(ctx context.Context, runtime framework.Runtime, kubectl *framework.KubectlClient) (*temporaryChecksInterval, error) {
	var deployments appsv1.DeploymentList
	if err := kubectl.GetJSON(ctx, &deployments, "get", "deployments", "-n", framework.SoperatorSystemNamespace,
		"-l", "control-plane=soperatorchecks", "-o", "json"); err != nil {
		return nil, fmt.Errorf("discover soperatorchecks Deployment: %w", err)
	}
	if len(deployments.Items) != 1 {
		return nil, fmt.Errorf("discover soperatorchecks Deployment: expected one Deployment, found %d", len(deployments.Items))
	}
	deployment := deployments.Items[0]
	if deployment.Name == "" || deployment.Namespace != framework.SoperatorSystemNamespace || deployment.UID == "" || !checksDeploymentReady(deployment) {
		return nil, fmt.Errorf("prepare soperatorchecks interval: Deployment %s/%s is not healthy: generation=%d observed=%d updated=%d ready=%d available=%d unavailable=%d",
			deployment.Namespace, deployment.Name, deployment.Generation, deployment.Status.ObservedGeneration,
			deployment.Status.UpdatedReplicas, deployment.Status.ReadyReplicas, deployment.Status.AvailableReplicas, deployment.Status.UnavailableReplicas)
	}
	index := -1
	for i, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == "manager" {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, fmt.Errorf("find manager container in soperatorchecks Deployment %s/%s", deployment.Namespace, deployment.Name)
	}
	args, changed, err := maintenanceChecksArgs(deployment.Spec.Template.Spec.Containers[index].Args)
	if err != nil || !changed {
		return nil, err
	}
	state := &temporaryChecksInterval{deployment: deployment, containerIndex: index, args: args}
	release, found, err := checksHelmRelease(ctx, kubectl, deployment)
	if err != nil {
		return state, err
	}
	if found {
		state.release = &release
		state.releaseOwned = true
		// Guard the child release against its umbrella chart's drift correction.
		if err := patchChecksResource(ctx, runtime, helmReleaseResource, state.release.Metadata, map[string]any{
			"metadata": map[string]any{"resourceVersion": state.release.Metadata.ResourceVersion,
				"annotations": map[string]any{helmDriftAnnotation: "disabled"}},
			"spec": map[string]any{"suspend": true},
		}, "merge"); err != nil {
			return state, fmt.Errorf("suspend soperatorchecks HelmRelease: %w", err)
		}
		if err := runtime.WaitFor(ctx, "soperatorchecks HelmRelease reconciliation paused", checksDeploymentTimeout, 5*time.Second,
			func(waitCtx context.Context) (bool, error) {
				var current helmRelease
				if err := kubectl.GetJSON(waitCtx, &current, "get", helmReleaseResource, state.release.Metadata.Name,
					"-n", state.release.Metadata.Namespace, "-o", "json"); err != nil {
					return false, err
				}
				if current.Metadata.UID != state.release.Metadata.UID || current.Spec.Suspend == nil || !*current.Spec.Suspend ||
					current.Metadata.Annotations[helmDriftAnnotation] != "disabled" {
					return false, fmt.Errorf("verify soperatorchecks HelmRelease %s/%s suspension and drift guard", current.Metadata.Namespace, current.Metadata.Name)
				}
				reconciling := findMetaCondition(current.Status.Conditions, "Reconciling")
				return reconciling == nil || reconciling.Status != metav1.ConditionTrue, nil
			}); err != nil {
			return state, err
		}
	}
	state.deploymentOwned = true
	if err := state.patchArgs(ctx, runtime, deployment, args); err != nil {
		return state, err
	}
	return state, state.waitForDeployment(ctx, runtime, kubectl, args)
}

func maintenanceChecksArgs(original []string) ([]string, bool, error) {
	args := slices.Clone(original)
	index, length := -1, 0
	interval := 3 * time.Minute
	for i, arg := range original {
		value, found := strings.CutPrefix(arg, checksSlurmNodesIntervalFlag+"=")
		count := 1
		if arg == checksSlurmNodesIntervalFlag {
			if i+1 >= len(original) {
				return nil, false, fmt.Errorf("parse soperatorchecks interval: %s has no value", checksSlurmNodesIntervalFlag)
			}
			value, found, count = original[i+1], true, 2
		}
		if !found {
			continue
		}
		if index >= 0 {
			return nil, false, fmt.Errorf("parse soperatorchecks interval: duplicate %s", checksSlurmNodesIntervalFlag)
		}
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 {
			return nil, false, fmt.Errorf("parse soperatorchecks interval %q: expected a positive duration", value)
		}
		index, length, interval = i, count, parsed
	}
	if interval <= checksMaintenanceInterval {
		return original, false, nil
	}
	if index < 0 {
		args = append(args, checksSlurmNodesIntervalFlag+"="+checksMaintenanceInterval.String())
	} else if length == 2 {
		args[index+1] = checksMaintenanceInterval.String()
	} else {
		args[index] = checksSlurmNodesIntervalFlag + "=" + checksMaintenanceInterval.String()
	}
	return args, true, nil
}

func checksHelmRelease(ctx context.Context, kubectl *framework.KubectlClient, deployment appsv1.Deployment) (helmRelease, bool, error) {
	releaseName := deployment.Annotations["meta.helm.sh/release-name"]
	releaseNamespace := deployment.Annotations["meta.helm.sh/release-namespace"]
	if releaseName == "" && releaseNamespace == "" {
		return helmRelease{}, false, nil
	}
	if releaseName == "" || releaseNamespace == "" {
		return helmRelease{}, false, fmt.Errorf("identify soperatorchecks Helm ownership: incomplete release annotations on Deployment %s/%s", deployment.Namespace, deployment.Name)
	}
	var releases helmReleaseList
	if err := kubectl.GetJSON(ctx, &releases, "get", helmReleaseResource, "-A", "-o", "json"); err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "doesn't have a resource type") || strings.Contains(message, "no matches for kind") {
			return helmRelease{}, false, nil
		}
		return helmRelease{}, false, fmt.Errorf("discover soperatorchecks HelmRelease: %w", err)
	}
	var matches []helmRelease
	for _, release := range releases.Items {
		targetNamespace := release.Spec.TargetNamespace
		if targetNamespace == "" {
			targetNamespace = release.Metadata.Namespace
		}
		name := release.Spec.ReleaseName
		if name == "" {
			name = release.Metadata.Name
			if release.Spec.TargetNamespace != "" {
				name = release.Spec.TargetNamespace + "-" + name
			}
		}
		if len(name) > 53 {
			name = fmt.Sprintf("%s-%x", name[:40], sha256.Sum256([]byte(name)))[:53]
		}
		if name == releaseName && targetNamespace == deployment.Namespace && targetNamespace == releaseNamespace {
			matches = append(matches, release)
		}
	}
	if len(matches) == 0 {
		return helmRelease{}, false, nil
	}
	if len(matches) != 1 {
		return helmRelease{}, false, fmt.Errorf("identify soperatorchecks HelmRelease for %s/%s: found %d matching releases", releaseNamespace, releaseName, len(matches))
	}
	release := matches[0]
	ready := findMetaCondition(release.Status.Conditions, "Ready")
	if release.Metadata.UID == "" || release.Metadata.DeletionTimestamp != nil || ready == nil || ready.Status != metav1.ConditionTrue {
		return helmRelease{}, false, fmt.Errorf("prepare soperatorchecks HelmRelease %s/%s: release is not Ready", release.Metadata.Namespace, release.Metadata.Name)
	}
	return release, true, nil
}

func (s *temporaryChecksInterval) restore(ctx context.Context, runtime framework.Runtime, kubectl *framework.KubectlClient) error {
	if s == nil {
		return nil
	}
	var problems []error
	if s.deploymentOwned {
		original := s.deployment.Spec.Template.Spec.Containers[s.containerIndex].Args
		var current appsv1.Deployment
		err := kubectl.GetJSON(ctx, &current, "get", "deployment", s.deployment.Name, "-n", s.deployment.Namespace, "-o", "json")
		if err == nil {
			if current.UID != s.deployment.UID || s.containerIndex >= len(current.Spec.Template.Spec.Containers) ||
				current.Spec.Template.Spec.Containers[s.containerIndex].Name != "manager" {
				err = fmt.Errorf("restore soperatorchecks interval: Deployment %s/%s or manager container identity changed", current.Namespace, current.Name)
			} else {
				args := current.Spec.Template.Spec.Containers[s.containerIndex].Args
				if !slices.Equal(args, original) {
					if !slices.Equal(args, s.args) {
						err = fmt.Errorf("restore soperatorchecks interval: manager arguments changed outside the scenario")
					} else {
						err = s.patchArgs(ctx, runtime, current, original)
					}
				}
				if err == nil {
					err = s.waitForDeployment(ctx, runtime, kubectl, original)
				}
			}
		}
		if err != nil {
			problems = append(problems, err)
		} else {
			s.deploymentOwned = false
		}
	}
	if s.releaseOwned {
		if err := s.restoreRelease(ctx, runtime, kubectl); err != nil {
			problems = append(problems, fmt.Errorf("restore soperatorchecks HelmRelease: %w", err))
		} else {
			s.releaseOwned = false
		}
	}
	return errors.Join(problems...)
}

func (s *temporaryChecksInterval) restoreRelease(ctx context.Context, runtime framework.Runtime, kubectl *framework.KubectlClient) error {
	var current helmRelease
	if err := kubectl.GetJSON(ctx, &current, "get", helmReleaseResource, s.release.Metadata.Name, "-n", s.release.Metadata.Namespace, "-o", "json"); err != nil {
		return err
	}
	if current.Metadata.UID != s.release.Metadata.UID {
		return fmt.Errorf("HelmRelease %s/%s resource identity changed", current.Metadata.Namespace, current.Metadata.Name)
	}
	suspendOriginal := optionalBoolEqual(current.Spec.Suspend, s.release.Spec.Suspend)
	suspendOwned := suspendOriginal || (current.Spec.Suspend != nil && *current.Spec.Suspend)
	originalAnnotation, originalExists := s.release.Metadata.Annotations[helmDriftAnnotation]
	currentAnnotation, currentExists := current.Metadata.Annotations[helmDriftAnnotation]
	annotationOriginal := currentExists == originalExists && currentAnnotation == originalAnnotation
	annotationOwned := annotationOriginal || currentAnnotation == "disabled"
	var problems []error
	patch := make(map[string]any)
	metadata := map[string]any{"resourceVersion": current.Metadata.ResourceVersion}
	if !suspendOwned {
		problems = append(problems, fmt.Errorf("HelmRelease suspension changed outside the scenario"))
	} else if !suspendOriginal {
		patch["spec"] = map[string]any{"suspend": s.release.Spec.Suspend}
	}
	if !annotationOwned {
		problems = append(problems, fmt.Errorf("HelmRelease drift annotation changed outside the scenario"))
	} else if !annotationOriginal {
		var annotation any
		if originalExists {
			annotation = originalAnnotation
		}
		metadata["annotations"] = map[string]any{helmDriftAnnotation: annotation}
	}
	if len(patch) > 0 || len(metadata) > 1 {
		patch["metadata"] = metadata
		if err := patchChecksResource(ctx, runtime, helmReleaseResource, current.Metadata, patch, "merge"); err != nil {
			return errors.Join(append(problems, err)...)
		}
	}
	current = helmRelease{}
	if err := kubectl.GetJSON(ctx, &current, "get", helmReleaseResource, s.release.Metadata.Name, "-n", s.release.Metadata.Namespace, "-o", "json"); err != nil {
		return errors.Join(append(problems, err)...)
	}
	if current.Metadata.UID != s.release.Metadata.UID {
		problems = append(problems, fmt.Errorf("HelmRelease %s/%s resource identity changed after restoration", current.Metadata.Namespace, current.Metadata.Name))
	} else {
		if suspendOwned && !optionalBoolEqual(current.Spec.Suspend, s.release.Spec.Suspend) {
			problems = append(problems, fmt.Errorf("HelmRelease suspension was not restored"))
		}
		annotation, exists := current.Metadata.Annotations[helmDriftAnnotation]
		if annotationOwned && (exists != originalExists || annotation != originalAnnotation) {
			problems = append(problems, fmt.Errorf("HelmRelease drift annotation was not restored"))
		}
	}
	return errors.Join(problems...)
}

func optionalBoolEqual(left, right *bool) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func (s *temporaryChecksInterval) patchArgs(ctx context.Context, runtime framework.Runtime, current appsv1.Deployment, args []string) error {
	path := fmt.Sprintf("/spec/template/spec/containers/%d/args", s.containerIndex)
	patch := []map[string]any{
		{"op": "test", "path": "/metadata/uid", "value": string(s.deployment.UID)},
		{"op": "test", "path": fmt.Sprintf("/spec/template/spec/containers/%d", s.containerIndex), "value": current.Spec.Template.Spec.Containers[s.containerIndex]},
		{"op": "add", "path": path, "value": args},
	}
	return patchChecksResource(ctx, runtime, "deployment", current.ObjectMeta, patch, "json")
}

func (s *temporaryChecksInterval) waitForDeployment(ctx context.Context, runtime framework.Runtime, kubectl *framework.KubectlClient, args []string) error {
	return runtime.WaitFor(ctx, "soperatorchecks Deployment ready with configured interval", checksDeploymentTimeout, 5*time.Second,
		func(waitCtx context.Context) (bool, error) {
			var current appsv1.Deployment
			if err := kubectl.GetJSON(waitCtx, &current, "get", "deployment", s.deployment.Name, "-n", s.deployment.Namespace, "-o", "json"); err != nil {
				return false, err
			}
			if current.UID != s.deployment.UID || s.containerIndex >= len(current.Spec.Template.Spec.Containers) ||
				current.Spec.Template.Spec.Containers[s.containerIndex].Name != "manager" ||
				!slices.Equal(current.Spec.Template.Spec.Containers[s.containerIndex].Args, args) {
				return false, fmt.Errorf("verify soperatorchecks Deployment %s/%s identity and manager arguments", current.Namespace, current.Name)
			}
			if !checksDeploymentReady(current) {
				return false, fmt.Errorf("soperatorchecks Deployment %s/%s pending: generation=%d observed=%d updated=%d ready=%d available=%d unavailable=%d",
					current.Namespace, current.Name, current.Generation, current.Status.ObservedGeneration, current.Status.UpdatedReplicas,
					current.Status.ReadyReplicas, current.Status.AvailableReplicas, current.Status.UnavailableReplicas)
			}
			return true, nil
		})
}

func checksDeploymentReady(deployment appsv1.Deployment) bool {
	replicas := int32(1)
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}
	status := deployment.Status
	return deployment.DeletionTimestamp == nil && replicas > 0 && status.ObservedGeneration >= deployment.Generation &&
		status.Replicas == replicas && status.UpdatedReplicas == replicas && status.ReadyReplicas == replicas &&
		status.AvailableReplicas == replicas && status.UnavailableReplicas == 0
}

func patchChecksResource(ctx context.Context, runtime framework.Runtime, resource string, metadata metav1.ObjectMeta, patch any, patchType string) error {
	data, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("encode %s %s/%s patch: %w", resource, metadata.Namespace, metadata.Name, err)
	}
	if _, err := runtime.Kubectl().Run(ctx, "patch", resource, metadata.Name, "-n", metadata.Namespace, "--type="+patchType, "-p", string(data)); err != nil {
		return fmt.Errorf("patch %s %s/%s: %w", resource, metadata.Namespace, metadata.Name, err)
	}
	return nil
}
