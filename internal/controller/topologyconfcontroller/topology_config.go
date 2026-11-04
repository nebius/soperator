package topologyconfcontroller

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"time"

	"github.com/go-logr/logr"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/topologyconfcontroller/deferral"
	"nebius.ai/slurm-operator/internal/controller/topologyconfcontroller/topology"
	"nebius.ai/slurm-operator/internal/render/common"
	renderutils "nebius.ai/slurm-operator/internal/render/utils"
	"nebius.ai/slurm-operator/internal/utils/jailedpayload"
	"nebius.ai/slurm-operator/internal/utils/resourcegetter"
)

// Annotations of the topology JailedConfig holding the deferral state: when the pending deferred
// reconfigure first became due and when its reason last changed, and when a reconfigure was last
// requested or confirmed. A pending request not newer than that was settled by it: content
// published under a standing request is reconfigured before the request can be confirmed.
const (
	annotationDeferredSince      = "topology.slurm.nebius.ai/deferred-since"
	annotationDeferredLastChange = "topology.slurm.nebius.ai/deferred-last-change"
	annotationLastReconfigure    = "topology.slurm.nebius.ai/last-reconfigure"
)

// topologyConfig is a cluster's topology.yaml as published to the jail: the ConfigMap holding the
// file and the JailedConfig that places it and carries the reconfigure request.
type topologyConfig struct {
	client              client.Client
	scheme              *runtime.Scheme
	key                 client.ObjectKey
	owner               *slurmv1.SlurmCluster
	reconfigureDeferral deferral.Policy
	jailedConfig        *v1alpha1.JailedConfig
	publishedHash       string
}

func newTopologyConfig(c client.Client, scheme *runtime.Scheme, cluster *slurmv1.SlurmCluster, reconfigureDeferral deferral.Policy) *topologyConfig {
	namePrefix := resourcegetter.ResolvePodNamePrefix(cluster.Name, cluster.Spec.PodNamePrefix)
	return &topologyConfig{
		client: c,
		scheme: scheme,
		key: client.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      resourcegetter.BuildPrefixedName(namePrefix, consts.ConfigMapNameTopologyConfig),
		},
		owner:               cluster,
		reconfigureDeferral: reconfigureDeferral,
	}
}

// currentTopology is the published topology, empty while nothing is published.
func (c *topologyConfig) currentTopology(ctx context.Context) (topology.MultiTopology, error) {
	if c.jailedConfig == nil {
		return nil, nil
	}
	configMapKey := client.ObjectKey{Namespace: c.key.Namespace, Name: c.jailedConfig.Spec.ConfigMap.Name}
	configMap := &corev1.ConfigMap{}
	if err := c.client.Get(ctx, configMapKey, configMap); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get ConfigMap %q: %w", configMapKey.Name, err)
	}

	content, ok := configMap.Data[consts.ConfigMapKeyTopologyYAML]
	if !ok {
		return nil, nil
	}
	payload, err := jailedpayload.Make(&c.jailedConfig.Spec, configMap)
	if err != nil {
		return nil, fmt.Errorf("render payload of JailedConfig %q: %w", c.key.Name, err)
	}
	c.publishedHash = jailedpayload.Hash(payload)

	var current topology.MultiTopology
	if err := yaml.Unmarshal([]byte(content), &current); err != nil {
		return nil, fmt.Errorf("parse %s of ConfigMap %q: %w", consts.ConfigMapKeyTopologyYAML, configMapKey.Name, err)
	}
	return current, nil
}

// publishTopology writes the topology to the ConfigMap, which sconfigcontroller picks up on its own.
func (c *topologyConfig) publishTopology(ctx context.Context, topo *topology.MultiTopology) error {
	content, err := yaml.Marshal(topo)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", consts.ConfigMapKeyTopologyYAML, err)
	}
	rendered := common.WithManagedSlurmConfigWarning(renderutils.NewAsIsConfig(string(content))).Render()

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: c.key.Name, Namespace: c.key.Namespace},
	}
	err = c.client.Get(ctx, c.key, configMap)
	if client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("get ConfigMap %q: %w", c.key.Name, err)
	}
	if err := controllerutil.SetControllerReference(c.owner, configMap, c.scheme); err != nil {
		return fmt.Errorf("set owner of ConfigMap %q: %w", c.key.Name, err)
	}
	configMap.Data = map[string]string{consts.ConfigMapKeyTopologyYAML: rendered}
	if apierrors.IsNotFound(err) {
		if err := c.client.Create(ctx, configMap); err != nil {
			return fmt.Errorf("create ConfigMap %q: %w", c.key.Name, err)
		}
		return nil
	}
	if err := c.client.Update(ctx, configMap); err != nil {
		return fmt.Errorf("update ConfigMap %q: %w", c.key.Name, err)
	}
	return nil
}

func (c *topologyConfig) requestReconfigure(ctx context.Context, logger logr.Logger) error {
	return c.update(ctx, logger, true)
}

// withdrawConfirmedReconfigure clears a confirmed request before new content is published: left
// in the spec, it would make sconfigcontroller reconfigure again on the content change.
func (c *topologyConfig) withdrawConfirmedReconfigure(ctx context.Context, logger logr.Logger) error {
	if c.jailedConfig == nil || !hasReconfigureRequest(c.jailedConfig) || !c.reconfigureConfirmed() {
		return nil
	}
	return c.update(ctx, logger, false)
}

// update creates or updates the JailedConfig, raising a reconfigure request or withdrawing the
// standing one, and remembers the written object.
func (c *topologyConfig) update(ctx context.Context, logger logr.Logger, reconfigure bool) error {
	logger = logger.WithValues("JailedConfig", c.key.Name)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	desired := &v1alpha1.JailedConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.key.Name,
			Namespace: c.key.Namespace,
			Labels: map[string]string{
				consts.LabelJailedAggregationKey: consts.LabelJailedAggregationCommonValue,
				consts.LabelInstanceKey:          c.owner.Name,
			},
		},
		Spec: v1alpha1.JailedConfigSpec{
			ConfigMap: v1alpha1.ConfigMapReference{Name: c.key.Name},
			Items: []corev1.KeyToPath{{
				Key:  consts.ConfigMapKeyTopologyYAML,
				Path: filepath.Join("/etc/slurm/", consts.ConfigMapKeyTopologyYAML),
			}},
			UpdateActions: []v1alpha1.UpdateAction{},
		},
	}

	existing := &v1alpha1.JailedConfig{}
	err := c.client.Get(ctx, c.key, existing)
	if apierrors.IsNotFound(err) {
		if reconfigure {
			logger.Info("Requesting a Slurm reconfigure")
			desired.Spec.UpdateActions = []v1alpha1.UpdateAction{v1alpha1.UpdateActionReconfigure}
			desired.Annotations = map[string]string{annotationLastReconfigure: now}
		}
		if err := controllerutil.SetControllerReference(c.owner, desired, c.scheme); err != nil {
			return fmt.Errorf("set owner of JailedConfig %q: %w", c.key.Name, err)
		}
		if err := c.client.Create(ctx, desired); err != nil {
			return fmt.Errorf("create JailedConfig %q: %w", c.key.Name, err)
		}
		c.jailedConfig = desired
		return nil
	}
	if err != nil {
		return fmt.Errorf("get JailedConfig %q: %w", c.key.Name, err)
	}

	if reconfigure {
		logger.Info("Requesting a Slurm reconfigure")
		desired.Spec.UpdateActions = []v1alpha1.UpdateAction{v1alpha1.UpdateActionReconfigure}
	} else if hasReconfigureRequest(existing) {
		logger.Info("Slurm reconfigure confirmed, withdrawing the request")
	}
	// Stamp on a repeated request too: it settles a deferred booking the standing request covers,
	// and the withdrawal that follows the actual reconfigure stamps a later time anyway.
	desired.Annotations = map[string]string{annotationLastReconfigure: now}

	if err := controllerutil.SetControllerReference(c.owner, existing, c.scheme); err != nil {
		return fmt.Errorf("set owner of JailedConfig %q: %w", c.key.Name, err)
	}
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	maps.Copy(existing.Labels, desired.Labels)
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	maps.Copy(existing.Annotations, desired.Annotations)
	existing.Spec = desired.Spec
	if err := c.client.Update(ctx, existing); err != nil {
		return fmt.Errorf("update JailedConfig %q: %w", c.key.Name, err)
	}
	c.jailedConfig = existing
	return nil
}

// deferReconfigure books a deferred reconfigure request for a change arriving now and returns how
// long it still has to wait, zero when it may run now. A due request is booked too, so a raise that
// fails after the publish is retried as a pending booking.
func (c *topologyConfig) deferReconfigure(ctx context.Context, now time.Time) (time.Duration, error) {
	if c.jailedConfig == nil {
		return 0, nil
	}
	lastRun := timeAnnotation(c.jailedConfig, annotationLastReconfigure)
	request := deferral.NewRequest(now)
	if since := timeAnnotation(c.jailedConfig, annotationDeferredSince); since.After(lastRun) {
		request = deferral.Request{
			Since:      since,
			LastChange: timeAnnotation(c.jailedConfig, annotationDeferredLastChange),
		}.Changed(now)
	}

	fireAt := c.reconfigureDeferral.FireTime(lastRun, request)
	patch := client.MergeFrom(c.jailedConfig.DeepCopy())
	if c.jailedConfig.Annotations == nil {
		c.jailedConfig.Annotations = map[string]string{}
	}
	c.jailedConfig.Annotations[annotationDeferredSince] = request.Since.UTC().Format(time.RFC3339Nano)
	c.jailedConfig.Annotations[annotationDeferredLastChange] = request.LastChange.UTC().Format(time.RFC3339Nano)
	if err := c.client.Patch(ctx, c.jailedConfig, patch); err != nil {
		return 0, fmt.Errorf("record deferred reconfigure on JailedConfig %q: %w", c.key.Name, err)
	}
	return max(fireAt.Sub(now), 0), nil
}

// pendingDeferredReconfigure tells whether a deferred reconfigure request is pending and how long
// it still has to wait, zero when it may run now.
func (c *topologyConfig) pendingDeferredReconfigure(now time.Time) (pending bool, wait time.Duration) {
	if c.jailedConfig == nil {
		return false, 0
	}
	lastRun := timeAnnotation(c.jailedConfig, annotationLastReconfigure)
	since := timeAnnotation(c.jailedConfig, annotationDeferredSince)
	if !since.After(lastRun) {
		return false, 0
	}

	request := deferral.Request{
		Since:      since,
		LastChange: timeAnnotation(c.jailedConfig, annotationDeferredLastChange),
	}
	fireAt := c.reconfigureDeferral.FireTime(lastRun, request)
	if !now.Before(fireAt) {
		return true, 0
	}
	return true, fireAt.Sub(now)
}

// timeAnnotation is the RFC3339 time in the annotation, zero when absent or unparsable.
func timeAnnotation(jailedConfig *v1alpha1.JailedConfig, key string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, jailedConfig.Annotations[key])
	if err != nil {
		return time.Time{}
	}
	return t
}

// reconfigureConfirmed reports whether a reconfigure ran over the published content: the
// ReconfigurePerformed condition is true for the current generation and the applied hash is the
// published file's.
func (c *topologyConfig) reconfigureConfirmed() bool {
	condition := meta.FindStatusCondition(c.jailedConfig.Status.Conditions, string(v1alpha1.ReconfigurePerformed))
	return condition != nil && condition.Status == metav1.ConditionTrue &&
		condition.ObservedGeneration == c.jailedConfig.Generation &&
		c.jailedConfig.Status.AppliedHash == c.publishedHash
}

func hasReconfigureRequest(jailedConfig *v1alpha1.JailedConfig) bool {
	for _, action := range jailedConfig.Spec.UpdateActions {
		if action == v1alpha1.UpdateActionReconfigure {
			return true
		}
	}
	return false
}
