package reconciler

import (
	"context"
	"testing"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/v25/api/v1alpha1"
	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/soperatorchecks"
)

const (
	testNamespace = "test-ns"
	testName      = "test-resource"

	operatorLabel      = consts.LabelComponentKey
	operatorAnnotation = consts.AnnotationDefaultContainerName
	foreignAnnotation  = "kubectl.kubernetes.io/restartedAt"
)

func newTestReconciler(t *testing.T) (*Reconciler, client.Client, *slurmv1.SlurmCluster) {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, kruisev1b1.AddToScheme(scheme))
	require.NoError(t, mariadbv1alpha1.AddToScheme(scheme))
	require.NoError(t, slurmv1.AddToScheme(scheme))

	cluster := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: testNamespace, UID: "cluster-uid"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	return NewReconciler(c, scheme, record.NewFakeRecorder(10)), c, cluster
}

// extras describes what user supplies via extraLabels/extraAnnotations.
type extras struct {
	labels      map[string]string
	annotations map[string]string
}

// metadataTarget adapts a workload kind to the shared metadata reconciliation scenario.
type metadataTarget struct {
	name string
	// render builds the desired resource the way renderers do: operator-owned keys first, extras on top of them
	// without overriding.
	render func(extras) client.Object
	// reconcile runs the reconciler of the kind.
	reconcile func(ctx context.Context, r *Reconciler, owner client.Object, desired client.Object) error
	// live returns the stored resource.
	live func() client.Object
	// podMeta returns labels and annotations of the pods the resource creates.
	podMeta func(client.Object) (labels, annotations map[string]string)
	// addForeign adds an annotation to the pod template the way kubectl rollout restart does.
	addForeign func(client.Object)
	// foreignSurvives is false for the kinds whose pod metadata is owned entirely by the operator.
	foreignSurvives bool
	// objectLabels returns the labels of the resource itself, if the renderer puts extras there.
	objectLabels func(client.Object) map[string]string
}

func renderedMeta(e extras) (labels, annotations map[string]string) {
	labels = map[string]string{operatorLabel: "operator", "app": "x"}
	annotations = map[string]string{operatorAnnotation: "main"}
	for k, v := range e.labels {
		if _, ok := labels[k]; !ok {
			labels[k] = v
		}
	}
	for k, v := range e.annotations {
		if _, ok := annotations[k]; !ok {
			annotations[k] = v
		}
	}
	return labels, annotations
}

func objectMeta(labels map[string]string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: testName, Namespace: testNamespace, Labels: labels}
}

func metadataTargets(c client.Client) []metadataTarget {
	key := client.ObjectKey{Namespace: testNamespace, Name: testName}
	get := func(obj client.Object) client.Object {
		if err := c.Get(context.Background(), key, obj); err != nil {
			panic(err)
		}
		return obj
	}
	foreign := func(m *metav1.ObjectMeta) {
		if m.Annotations == nil {
			m.Annotations = map[string]string{}
		}
		m.Annotations[foreignAnnotation] = "2026-01-01T00:00:00Z"
	}

	return []metadataTarget{
		{
			name: "Deployment",
			render: func(e extras) client.Object {
				l, a := renderedMeta(e)
				return &appsv1.Deployment{
					ObjectMeta: objectMeta(l),
					Spec: appsv1.DeploymentSpec{
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{operatorLabel: "operator"}},
						Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: l, Annotations: a}},
					},
				}
			},
			reconcile: func(ctx context.Context, r *Reconciler, owner, desired client.Object) error {
				return NewDeploymentReconciler(r).Reconcile(ctx, owner, *desired.(*appsv1.Deployment))
			},
			live: func() client.Object { return get(&appsv1.Deployment{}) },
			podMeta: func(o client.Object) (map[string]string, map[string]string) {
				t := o.(*appsv1.Deployment).Spec.Template
				return t.Labels, t.Annotations
			},
			addForeign:      func(o client.Object) { foreign(&o.(*appsv1.Deployment).Spec.Template.ObjectMeta) },
			foreignSurvives: true,
		},
		{
			name: "StatefulSet",
			render: func(e extras) client.Object {
				l, a := renderedMeta(e)
				return &appsv1.StatefulSet{
					ObjectMeta: objectMeta(l),
					Spec: appsv1.StatefulSetSpec{
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{operatorLabel: "operator"}},
						Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: l, Annotations: a}},
					},
				}
			},
			reconcile: func(ctx context.Context, r *Reconciler, owner, desired client.Object) error {
				return NewStatefulSetReconciler(r).Reconcile(ctx, owner, desired.(*appsv1.StatefulSet))
			},
			live: func() client.Object { return get(&appsv1.StatefulSet{}) },
			podMeta: func(o client.Object) (map[string]string, map[string]string) {
				t := o.(*appsv1.StatefulSet).Spec.Template
				return t.Labels, t.Annotations
			},
			addForeign:      func(o client.Object) { foreign(&o.(*appsv1.StatefulSet).Spec.Template.ObjectMeta) },
			foreignSurvives: true,
		},
		{
			name: "AdvancedStatefulSet",
			render: func(e extras) client.Object {
				l, a := renderedMeta(e)
				return &kruisev1b1.StatefulSet{
					ObjectMeta: objectMeta(l),
					Spec: kruisev1b1.StatefulSetSpec{
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{operatorLabel: "operator"}},
						Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: l, Annotations: a}},
					},
				}
			},
			reconcile: func(ctx context.Context, r *Reconciler, owner, desired client.Object) error {
				return NewAdvancedStatefulSetReconciler(r).Reconcile(ctx, owner, desired.(*kruisev1b1.StatefulSet))
			},
			live: func() client.Object { return get(&kruisev1b1.StatefulSet{}) },
			podMeta: func(o client.Object) (map[string]string, map[string]string) {
				t := o.(*kruisev1b1.StatefulSet).Spec.Template
				return t.Labels, t.Annotations
			},
			addForeign:      func(o client.Object) { foreign(&o.(*kruisev1b1.StatefulSet).Spec.Template.ObjectMeta) },
			foreignSurvives: true,
			objectLabels:    func(o client.Object) map[string]string { return o.GetLabels() },
		},
		{
			name: "CronJob",
			render: func(e extras) client.Object {
				l, a := renderedMeta(e)
				return &batchv1.CronJob{
					ObjectMeta: objectMeta(l),
					Spec: batchv1.CronJobSpec{
						JobTemplate: batchv1.JobTemplateSpec{
							Spec: batchv1.JobSpec{
								Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: l, Annotations: a}},
							},
						},
					},
				}
			},
			reconcile: func(ctx context.Context, r *Reconciler, owner, desired client.Object) error {
				return NewCronJobReconciler(r).Reconcile(ctx, owner, desired.(*batchv1.CronJob))
			},
			live: func() client.Object { return get(&batchv1.CronJob{}) },
			podMeta: func(o client.Object) (map[string]string, map[string]string) {
				t := o.(*batchv1.CronJob).Spec.JobTemplate.Spec.Template
				return t.Labels, t.Annotations
			},
			addForeign: func(o client.Object) {
				foreign(&o.(*batchv1.CronJob).Spec.JobTemplate.Spec.Template.ObjectMeta)
			},
			foreignSurvives: true,
			objectLabels:    func(o client.Object) map[string]string { return o.GetLabels() },
		},
		{
			name: "MariaDB",
			render: func(e extras) client.Object {
				l, a := renderedMeta(e)
				return &mariadbv1alpha1.MariaDB{
					ObjectMeta: objectMeta(l),
					Spec: mariadbv1alpha1.MariaDBSpec{
						PodTemplate: mariadbv1alpha1.PodTemplate{
							PodMetadata: &mariadbv1alpha1.Metadata{Labels: l, Annotations: a},
						},
					},
				}
			},
			reconcile: func(ctx context.Context, r *Reconciler, owner, desired client.Object) error {
				return NewMariaDbReconciler(r).Reconcile(
					ctx, owner.(*slurmv1.SlurmCluster), desired.(*mariadbv1alpha1.MariaDB), nil,
				)
			},
			live: func() client.Object { return get(&mariadbv1alpha1.MariaDB{}) },
			podMeta: func(o client.Object) (map[string]string, map[string]string) {
				m := o.(*mariadbv1alpha1.MariaDB).Spec.PodTemplate.PodMetadata
				if m == nil {
					return nil, nil
				}
				return m.Labels, m.Annotations
			},
			addForeign: func(client.Object) {},
		},
	}
}

func TestReconcile_ExtraLabelsAndAnnotations(t *testing.T) {
	ctx := context.Background()

	steps := []struct {
		name string
		in   extras
		// wantLabels/wantAnnotations are the extras expected on the pods in addition to the operator-owned keys.
		wantLabels      map[string]string
		wantAnnotations map[string]string
		absentLabels    []string
		absent          []string
	}{
		{
			name: "additions",
			in: extras{
				labels:      map[string]string{"team": "a", "env": "dev"},
				annotations: map[string]string{"note": "one", "owner": "me"},
			},
			wantLabels:      map[string]string{"team": "a", "env": "dev"},
			wantAnnotations: map[string]string{"note": "one", "owner": "me"},
		},
		{
			name: "changes, additions and removals at once",
			in: extras{
				labels:      map[string]string{"team": "a", "env": "prod", "new": "x"},
				annotations: map[string]string{"note": "two", "added": "yes"},
			},
			wantLabels:      map[string]string{"team": "a", "env": "prod", "new": "x"},
			wantAnnotations: map[string]string{"note": "two", "added": "yes"},
			absent:          []string{"owner"},
		},
		{
			name: "protected keys are not overridden",
			in: extras{
				labels:      map[string]string{operatorLabel: "hijacked", "team": "a"},
				annotations: map[string]string{operatorAnnotation: "hijacked", "note": "two"},
			},
			wantLabels:      map[string]string{operatorLabel: "operator", "team": "a"},
			wantAnnotations: map[string]string{operatorAnnotation: "main", "note": "two"},
			absent:          []string{"added"},
			absentLabels:    []string{"env", "new"},
		},
		{
			name:            "all extras removed",
			in:              extras{},
			wantLabels:      map[string]string{operatorLabel: "operator", "app": "x"},
			wantAnnotations: map[string]string{operatorAnnotation: "main"},
			absent:          []string{"note", "owner", "added"},
			absentLabels:    []string{"team", "env", "new"},
		},
	}

	for _, kind := range []string{"Deployment", "StatefulSet", "AdvancedStatefulSet", "CronJob", "MariaDB"} {
		t.Run(kind, func(t *testing.T) {
			r, c, cluster := newTestReconciler(t)
			var target metadataTarget
			for _, tg := range metadataTargets(c) {
				if tg.name == kind {
					target = tg
				}
			}

			// The existing resource is created without any extras.
			require.NoError(t, target.reconcile(ctx, r, cluster, target.render(extras{})))
			live := target.live()
			target.addForeign(live)
			require.NoError(t, c.Update(ctx, live))

			for _, step := range steps {
				t.Run(step.name, func(t *testing.T) {
					require.NoError(t, target.reconcile(ctx, r, cluster, target.render(step.in)))

					live := target.live()
					labels, annotations := target.podMeta(live)
					for k, v := range step.wantLabels {
						assert.Equal(t, v, labels[k], "pod label %q", k)
					}
					for k, v := range step.wantAnnotations {
						assert.Equal(t, v, annotations[k], "pod annotation %q", k)
					}
					for _, k := range step.absentLabels {
						assert.NotContains(t, labels, k, "pod label %q must be removed", k)
					}
					for _, k := range step.absent {
						assert.NotContains(t, annotations, k, "pod annotation %q must be removed", k)
					}
					if target.foreignSurvives {
						assert.Contains(t, annotations, foreignAnnotation, "annotation set by others must be kept")
					}
					if target.objectLabels != nil {
						objLabels := target.objectLabels(live)
						for k, v := range step.wantLabels {
							assert.Equal(t, v, objLabels[k], "object label %q", k)
						}
						for _, k := range step.absentLabels {
							assert.NotContains(t, objLabels, k, "object label %q must be removed", k)
						}
					}
				})
			}
		})
	}
}

func TestReconcile_KeepsLabelsSetByOthersOnWorkloadObject(t *testing.T) {
	ctx := context.Background()
	r, c, cluster := newTestReconciler(t)
	reconciler := NewAdvancedStatefulSetReconciler(r)
	targets := metadataTargets(c)
	render := func(e extras) *kruisev1b1.StatefulSet { return targets[2].render(e).(*kruisev1b1.StatefulSet) }

	require.NoError(t, reconciler.Reconcile(ctx, cluster, render(extras{labels: map[string]string{"team": "a"}})))

	live := &kruisev1b1.StatefulSet{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testName}, live))
	live.Labels["argocd.argoproj.io/instance"] = "soperator"
	require.NoError(t, c.Update(ctx, live))

	require.NoError(t, reconciler.Reconcile(ctx, cluster, render(extras{})))

	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testName}, live))
	assert.Equal(t, "soperator", live.Labels["argocd.argoproj.io/instance"])
	assert.NotContains(t, live.Labels, "team")
}

func TestReconcile_PreservesDependencyVersionsAnnotationOnPodTemplate(t *testing.T) {
	ctx := context.Background()
	r, c, cluster := newTestReconciler(t)

	dep := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "dep", Namespace: testNamespace}}
	require.NoError(t, c.Create(ctx, dep))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(dep), dep))

	target := metadataTargets(c)[0]
	desired := func(e extras) appsv1.Deployment { return *target.render(e).(*appsv1.Deployment) }

	reconciler := NewDeploymentReconciler(r)
	d := desired(extras{annotations: map[string]string{"note": "one"}})
	require.NoError(t, reconciler.Reconcile(ctx, cluster, d, dep))
	d = desired(extras{})
	require.NoError(t, reconciler.Reconcile(ctx, cluster, d, dep))

	_, annotations := target.podMeta(target.live())
	assert.Contains(t, annotations, consts.AnnotationVersions)
	assert.NotContains(t, annotations, "note")
}

func TestReconcile_ActiveCheckCronJobKeepsOperatorOwnedAnnotation(t *testing.T) {
	ctx := context.Background()
	r, c, cluster := newTestReconciler(t)
	cronJobReconciler := NewCronJobReconciler(r)

	check := &slurmv1alpha1.ActiveCheck{
		ObjectMeta: metav1.ObjectMeta{Name: "test-check", Namespace: testNamespace},
		Spec: slurmv1alpha1.ActiveCheckSpec{
			Name:                "test-check",
			SlurmClusterRefName: cluster.Name,
			CheckType:           "k8sJob",
		},
	}
	reconcileCheck := func(extraLabels, extraAnnotations map[string]string) {
		desired, err := soperatorchecks.RenderK8sCronJob(check, nil, extraLabels, extraAnnotations)
		require.NoError(t, err)
		require.NoError(t, cronJobReconciler.Reconcile(ctx, cluster, &desired))
	}
	podMeta := func() (map[string]string, map[string]string) {
		cronJob := &batchv1.CronJob{}
		require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: check.Name}, cronJob))
		tmpl := cronJob.Spec.JobTemplate.Spec.Template
		return tmpl.Labels, tmpl.Annotations
	}

	reconcileCheck(nil, nil)
	_, annotations := podMeta()
	require.Equal(t, check.Name, annotations[consts.AnnotationActiveCheckName])

	reconcileCheck(
		map[string]string{"gcore.com/project-id": "1", consts.LabelComponentKey: "hijacked"},
		map[string]string{"gcore.com/note": "abc", consts.AnnotationActiveCheckName: "hijacked"},
	)
	labels, annotations := podMeta()
	assert.Equal(t, check.Name, annotations[consts.AnnotationActiveCheckName])
	assert.Equal(t, "abc", annotations["gcore.com/note"])
	assert.Equal(t, "1", labels["gcore.com/project-id"])
	assert.Equal(t, consts.ComponentTypeSoperatorChecks.String(), labels[consts.LabelComponentKey])

	reconcileCheck(nil, nil)
	labels, annotations = podMeta()
	assert.Equal(t, check.Name, annotations[consts.AnnotationActiveCheckName])
	assert.NotContains(t, annotations, "gcore.com/note")
	assert.NotContains(t, labels, "gcore.com/project-id")
}
