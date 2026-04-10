package soperatorchecks

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, batchv1.AddToScheme(scheme))
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))
	require.NoError(t, slurmv1.AddToScheme(scheme))
	return scheme
}

// TestActiveCheckJobReconciler_SkippedAnnotation verifies that when the
// slurm_check_job entrypoint annotates its owning Job with the
// slurm-skipped-reason annotation (indicating the cluster has no GPU),
// the reconciler sets LastRunStatus=Skipped and marks the Job final.
func TestActiveCheckJobReconciler_SkippedAnnotation(t *testing.T) {
	scheme := newTestScheme(t)
	ctx := context.Background()

	const (
		ns              = "soperator"
		activeCheckName = "cuda-samples"
		jobName         = "cuda-samples-12345"
		podName         = "cuda-samples-12345-abcde"
		skipReason      = "no GPU nodes in slurm.conf"
	)

	activeCheck := &slurmv1alpha1.ActiveCheck{
		ObjectMeta: metav1.ObjectMeta{Name: activeCheckName, Namespace: ns},
		Spec: slurmv1alpha1.ActiveCheckSpec{
			Name:                activeCheckName,
			SlurmClusterRefName: "slurm",
			CheckType:           "slurmJob",
		},
	}

	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: activeCheckName, Namespace: ns},
		Status: batchv1.CronJobStatus{
			LastScheduleTime: &metav1.Time{Time: time.Now()},
		},
	}

	k8sJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: ns,
			Annotations: map[string]string{
				consts.ActiveCheckSkippedReasonAnnotation: skipReason,
			},
			Labels: map[string]string{
				consts.LabelComponentKey: consts.ComponentTypeSoperatorChecks.String(),
			},
		},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podName,
			Namespace:   ns,
			Labels:      map[string]string{"job-name": jobName},
			Annotations: map[string]string{consts.AnnotationActiveCheckName: activeCheckName},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(activeCheck, cronJob, k8sJob, pod).
		WithStatusSubresource(&slurmv1alpha1.ActiveCheck{}).
		Build()

	baseReconciler := reconciler.NewReconciler(client, scheme, record.NewFakeRecorder(10))
	r := &ActiveCheckJobReconciler{
		Reconciler:       baseReconciler,
		Job:              reconciler.NewJobReconciler(baseReconciler),
		reconcileTimeout: 30 * time.Second,
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: jobName, Namespace: ns}}
	_, err := r.Reconcile(ctx, req)
	require.NoError(t, err)

	updated := &slurmv1alpha1.ActiveCheck{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: activeCheckName, Namespace: ns}, updated))
	assert.Equal(t, consts.ActiveCheckSlurmRunStatusSkipped, updated.Status.SlurmJobsStatus.LastRunStatus)
	assert.Equal(t, jobName, updated.Status.SlurmJobsStatus.LastRunName)
	assert.Empty(t, updated.Status.SlurmJobsStatus.LastRunId)

	updatedJob := &batchv1.Job{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: jobName, Namespace: ns}, updatedJob))
	assert.NotEmpty(t, updatedJob.Annotations[K8sAnnotationSoperatorChecksFinalStateTime],
		"final-state-time annotation should be set to suppress reprocessing")
}

// TestActiveCheckJobReconciler_SkippedAnnotation_Idempotent verifies that if
// the skipped path has already run once (the final-state-time annotation is
// set), a second reconcile is a no-op and does not touch the ActiveCheck
// status again.
func TestActiveCheckJobReconciler_SkippedAnnotation_Idempotent(t *testing.T) {
	scheme := newTestScheme(t)
	ctx := context.Background()

	const (
		ns              = "soperator"
		activeCheckName = "cuda-samples"
		jobName         = "cuda-samples-12345"
		podName         = "cuda-samples-12345-abcde"
	)

	activeCheck := &slurmv1alpha1.ActiveCheck{
		ObjectMeta: metav1.ObjectMeta{Name: activeCheckName, Namespace: ns},
		Spec: slurmv1alpha1.ActiveCheckSpec{
			Name:                activeCheckName,
			SlurmClusterRefName: "slurm",
			CheckType:           "slurmJob",
		},
	}
	// Sentinel value on the Job to prove idempotency: if the reconciler
	// re-entered the skipped path, it would overwrite this annotation with
	// a fresh timestamp.
	const preExistingFinalTime = "1234567890"

	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: activeCheckName, Namespace: ns},
	}

	k8sJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: ns,
			Annotations: map[string]string{
				consts.ActiveCheckSkippedReasonAnnotation:  "no GPU nodes in slurm.conf",
				K8sAnnotationSoperatorChecksFinalStateTime: preExistingFinalTime,
			},
			Labels: map[string]string{
				consts.LabelComponentKey: consts.ComponentTypeSoperatorChecks.String(),
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podName,
			Namespace:   ns,
			Labels:      map[string]string{"job-name": jobName},
			Annotations: map[string]string{consts.AnnotationActiveCheckName: activeCheckName},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(activeCheck, cronJob, k8sJob, pod).
		WithStatusSubresource(&slurmv1alpha1.ActiveCheck{}).
		Build()

	baseReconciler := reconciler.NewReconciler(client, scheme, record.NewFakeRecorder(10))
	r := &ActiveCheckJobReconciler{
		Reconciler:       baseReconciler,
		Job:              reconciler.NewJobReconciler(baseReconciler),
		reconcileTimeout: 30 * time.Second,
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: jobName, Namespace: ns}}
	_, err := r.Reconcile(ctx, req)
	require.NoError(t, err)

	// Final-state annotation must be unchanged — if the reconciler re-entered
	// the skipped path, it would have patched this with a fresh Unix timestamp.
	updatedJob := &batchv1.Job{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: jobName, Namespace: ns}, updatedJob))
	assert.Equal(t, preExistingFinalTime, updatedJob.Annotations[K8sAnnotationSoperatorChecksFinalStateTime],
		"final-state annotation should not be touched once the Job is already final")

	// ActiveCheck status was never seeded, so if the reconciler had re-entered
	// the skipped path, LastRunStatus would be Skipped. It should remain empty.
	updated := &slurmv1alpha1.ActiveCheck{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: activeCheckName, Namespace: ns}, updated))
	assert.Empty(t, updated.Status.SlurmJobsStatus.LastRunStatus,
		"status should not be touched once the Job is already final")
}

// TestActiveCheckReconciler_DependsOn_SkippedAsSuccess verifies that a
// prerequisite ActiveCheck with LastRunStatus=Skipped is treated as success
// by the dependsOn resolver, allowing dependent checks to proceed.
func TestActiveCheckReconciler_DependsOn_SkippedAsSuccess(t *testing.T) {
	scheme := newTestScheme(t)
	ctx := context.Background()
	logger := ctrl.Log.WithName("test")

	const ns = "soperator"

	availablePhase := slurmv1.PhaseClusterAvailable
	slurmCluster := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "slurm", Namespace: ns},
		Status:     slurmv1.SlurmClusterStatus{Phase: &availablePhase},
	}

	runAfterCreation := true
	prerequisite := &slurmv1alpha1.ActiveCheck{
		ObjectMeta: metav1.ObjectMeta{Name: "prepull-container-image", Namespace: ns},
		Spec: slurmv1alpha1.ActiveCheckSpec{
			Name:             "prepull-container-image",
			CheckType:        "slurmJob",
			RunAfterCreation: &runAfterCreation,
		},
		Status: slurmv1alpha1.ActiveCheckStatus{
			SlurmJobsStatus: slurmv1alpha1.ActiveCheckSlurmJobsStatus{
				LastRunStatus: consts.ActiveCheckSlurmRunStatusSkipped,
			},
		},
	}

	dependent := &slurmv1alpha1.ActiveCheck{
		ObjectMeta: metav1.ObjectMeta{Name: "cuda-samples", Namespace: ns},
		Spec: slurmv1alpha1.ActiveCheckSpec{
			Name:      "cuda-samples",
			CheckType: "slurmJob",
			DependsOn: []string{"prepull-container-image"},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(prerequisite, dependent).
		Build()

	baseReconciler := reconciler.NewReconciler(client, scheme, record.NewFakeRecorder(10))
	r := &ActiveCheckReconciler{Reconciler: baseReconciler}

	ready, err := r.dependenciesReady(ctx, logger, dependent, slurmCluster)
	require.NoError(t, err)
	assert.True(t, ready, "dependent check should unblock when prerequisite is Skipped")
}
