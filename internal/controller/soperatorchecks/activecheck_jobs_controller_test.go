package soperatorchecks

import (
	"context"
	"fmt"
	"testing"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

func TestActiveCheckJobReconciler_Reconcile_DoesNotFinalizeUntilAllSlurmJobsFinish(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	scheme := runtime.NewScheme()
	require.NoError(t, batchv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))

	const (
		namespace       = "test-ns"
		activeCheckName = "gpu-check"
		k8sJobName      = "gpu-check-123"
		firstSlurmJobID = "101"
		nextSlurmJobID  = "102"
	)

	submitTime := metav1.NewTime(time.Date(2026, time.April, 13, 10, 0, 0, 0, time.UTC))
	firstEndTime := metav1.NewTime(submitTime.Add(2 * time.Minute))
	secondEndTime := metav1.NewTime(submitTime.Add(4 * time.Minute))
	cronScheduleTime := metav1.NewTime(submitTime.Add(-time.Minute))

	activeCheck := &slurmv1alpha1.ActiveCheck{
		ObjectMeta: metav1.ObjectMeta{
			Name:      activeCheckName,
			Namespace: namespace,
		},
		Spec: slurmv1alpha1.ActiveCheckSpec{
			Name:                activeCheckName,
			CheckType:           "slurmJob",
			SlurmClusterRefName: "cluster-a",
		},
	}
	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      activeCheckName,
			Namespace: namespace,
		},
		Status: batchv1.CronJobStatus{
			LastScheduleTime: &cronScheduleTime,
		},
	}
	k8sJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sJobName,
			Namespace: namespace,
			Labels: map[string]string{
				consts.LabelComponentKey: consts.ComponentTypeSoperatorChecks.String(),
			},
			Annotations: map[string]string{
				"slurm-job-id": firstSlurmJobID + "," + nextSlurmJobID,
			},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gpu-check-pod",
			Namespace: namespace,
			Labels: map[string]string{
				"job-name": k8sJobName,
			},
			Annotations: map[string]string{
				consts.AnnotationActiveCheckName: activeCheckName,
			},
		},
	}

	fakeClient := clientfake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(activeCheck).
		WithObjects(activeCheck, cronJob, k8sJob, pod).
		Build()

	mockClient := slurmapifake.NewMockClient(t)
	reconcileRound := 1

	mockClient.EXPECT().
		GetJobsByID(mock.Anything, firstSlurmJobID).
		RunAndReturn(func(context.Context, string) ([]slurmapi.Job, error) {
			return []slurmapi.Job{{
				ID:         101,
				Name:       activeCheckName,
				State:      string(api.V0041JobInfoJobStateCOMPLETED),
				SubmitTime: &submitTime,
				EndTime:    &firstEndTime,
			}}, nil
		}).
		Twice()

	mockClient.EXPECT().
		GetJobsByID(mock.Anything, nextSlurmJobID).
		RunAndReturn(func(context.Context, string) ([]slurmapi.Job, error) {
			if reconcileRound == 1 {
				return []slurmapi.Job{{
					ID:         102,
					Name:       activeCheckName,
					State:      "RUNNING",
					SubmitTime: &submitTime,
					EndTime:    nil,
				}}, nil
			}

			return []slurmapi.Job{{
				ID:          102,
				Name:        activeCheckName,
				State:       string(api.V0041JobInfoJobStateFAILED),
				StateReason: "node lost",
				SubmitTime:  &submitTime,
				EndTime:     &secondEndTime,
			}}, nil
		}).
		Twice()

	slurmClients := slurmapi.NewClientSet(context.Background())
	slurmClients.AddClient(types.NamespacedName{
		Namespace: namespace,
		Name:      activeCheck.Spec.SlurmClusterRefName,
	}, mockClient)

	reconciler := NewActiveCheckJobController(
		fakeClient,
		scheme,
		record.NewFakeRecorder(10),
		slurmClients,
		time.Minute,
	)

	req := ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: namespace,
		Name:      k8sJobName,
	}}

	firstResult, err := reconciler.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.True(t, firstResult.Requeue)

	updatedJob := &batchv1.Job{}
	require.NoError(t, fakeClient.Get(ctx, req.NamespacedName, updatedJob))
	assert.Equal(
		t,
		fmt.Sprintf("%d", firstEndTime.Unix()),
		updatedJob.Annotations[K8sAnnotationSoperatorChecksFinalStateTime],
		"the Kubernetes job should store the latest handled Slurm end time as a watermark",
	)

	updatedCheck := &slurmv1alpha1.ActiveCheck{}
	require.NoError(t, fakeClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      activeCheckName,
	}, updatedCheck))
	assert.Equal(t, consts.ActiveCheckSlurmRunStatusInProgress, updatedCheck.Status.SlurmJobsStatus.LastRunStatus)

	reconcileRound = 2

	secondResult, err := reconciler.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.False(t, secondResult.Requeue)

	require.NoError(t, fakeClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      activeCheckName,
	}, updatedCheck))
	require.NoError(t, fakeClient.Get(ctx, req.NamespacedName, updatedJob))
	assert.Equal(t, fmt.Sprintf("%d", secondEndTime.Unix()), updatedJob.Annotations[K8sAnnotationSoperatorChecksFinalStateTime])
	assert.Equal(
		t,
		consts.ActiveCheckSlurmRunStatusFailed,
		updatedCheck.Status.SlurmJobsStatus.LastRunStatus,
		"the later failed Slurm job should still contribute to the final aggregated status",
	)
	assert.Equal(t, []slurmv1alpha1.JobAndReason{{
		JobID:  nextSlurmJobID,
		Reason: "node lost",
	}}, updatedCheck.Status.SlurmJobsStatus.LastRunFailJobsAndReasons)
}

func TestDeriveSlurmRunStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		requeue             bool
		failJobsAndReasons  []slurmv1alpha1.JobAndReason
		errorJobsAndReasons []slurmv1alpha1.JobAndReason
		cancelledJobs       []string
		expected            consts.ActiveCheckSlurmRunStatus
	}{
		{
			name:     "requeue takes precedence",
			requeue:  true,
			expected: consts.ActiveCheckSlurmRunStatusInProgress,
			failJobsAndReasons: []slurmv1alpha1.JobAndReason{{
				JobID:  "101",
				Reason: "boom",
			}},
		},
		{
			name: "failed takes precedence over error and cancelled",
			failJobsAndReasons: []slurmv1alpha1.JobAndReason{{
				JobID:  "101",
				Reason: "boom",
			}},
			errorJobsAndReasons: []slurmv1alpha1.JobAndReason{{
				JobID:  "102",
				Reason: "timeout",
			}},
			cancelledJobs: []string{"103"},
			expected:      consts.ActiveCheckSlurmRunStatusFailed,
		},
		{
			name: "error takes precedence over cancelled",
			errorJobsAndReasons: []slurmv1alpha1.JobAndReason{{
				JobID:  "102",
				Reason: "timeout",
			}},
			cancelledJobs: []string{"103"},
			expected:      consts.ActiveCheckSlurmRunStatusError,
		},
		{
			name:          "cancelled without failures or errors",
			cancelledJobs: []string{"103"},
			expected:      consts.ActiveCheckSlurmRunStatusCancelled,
		},
		{
			name:     "complete when nothing else applies",
			expected: consts.ActiveCheckSlurmRunStatusComplete,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := deriveSlurmRunStatus(tt.requeue, tt.failJobsAndReasons, tt.errorJobsAndReasons, tt.cancelledJobs)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestActiveCheckJobReconciler_Reconcile_SlurmJobSubmissionFailureSetsErrorStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := newActiveCheckJobTestScheme(t)

	activeCheck, cronJob, k8sJob, pod := newActiveCheckJobTestObjects("gpu-check", "gpu-check-123", "slurmJob")
	cronScheduleTime := metav1.NewTime(time.Date(2026, time.April, 13, 9, 59, 0, 0, time.UTC))
	cronJob.Status.LastScheduleTime = &cronScheduleTime
	k8sJob.Status.Conditions = []batchv1.JobCondition{{
		Type:   batchv1.JobFailed,
		Status: corev1.ConditionTrue,
	}}

	reconciler, fakeClient := newActiveCheckJobTestReconciler(t, scheme, activeCheck, cronJob, k8sJob, pod, nil)

	result, err := reconciler.Reconcile(ctx, newActiveCheckJobRequest(k8sJob))
	require.NoError(t, err)
	assert.False(t, result.Requeue)

	updatedCheck := &slurmv1alpha1.ActiveCheck{}
	require.NoError(t, fakeClient.Get(ctx, client.ObjectKeyFromObject(activeCheck), updatedCheck))
	assert.Equal(t, "No slurm job", updatedCheck.Status.SlurmJobsStatus.LastRunId)
	assert.Equal(t, k8sJob.Name, updatedCheck.Status.SlurmJobsStatus.LastRunName)
	assert.Equal(t, consts.ActiveCheckSlurmRunStatusError, updatedCheck.Status.SlurmJobsStatus.LastRunStatus)
	require.NotNil(t, updatedCheck.Status.SlurmJobsStatus.LastRunSubmitTime)
	assert.Equal(t, cronJob.Status.LastScheduleTime.Unix(), updatedCheck.Status.SlurmJobsStatus.LastRunSubmitTime.Unix())
	assert.False(t, updatedCheck.Status.SlurmJobsStatus.LastTransitionTime.IsZero())
}

func TestActiveCheckJobReconciler_Reconcile_SlurmJobAggregatesTerminalResultsInSingleReconcile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := newActiveCheckJobTestScheme(t)

	activeCheck, cronJob, k8sJob, pod := newActiveCheckJobTestObjects("gpu-check", "gpu-check-123", "slurmJob")
	k8sJob.Annotations["slurm-job-id"] = "101,102,103,104"

	submitTime := metav1.NewTime(time.Date(2026, time.April, 13, 10, 0, 0, 0, time.UTC))
	failedEndTime := metav1.NewTime(submitTime.Add(1 * time.Minute))
	cancelledEndTime := metav1.NewTime(submitTime.Add(2 * time.Minute))
	completedEndTime := metav1.NewTime(submitTime.Add(3 * time.Minute))
	errorEndTime := metav1.NewTime(submitTime.Add(4 * time.Minute))

	mockClient := slurmapifake.NewMockClient(t)
	mockClient.EXPECT().GetJobsByID(mock.Anything, "101").Return([]slurmapi.Job{{
		ID:          101,
		Name:        activeCheck.Name,
		State:       string(api.V0041JobInfoJobStateFAILED),
		StateReason: "boom",
		SubmitTime:  &submitTime,
		EndTime:     &failedEndTime,
	}}, nil).Once()
	mockClient.EXPECT().GetJobsByID(mock.Anything, "102").Return([]slurmapi.Job{{
		ID:         102,
		Name:       activeCheck.Name,
		State:      string(api.V0041JobInfoJobStateCANCELLED),
		SubmitTime: &submitTime,
		EndTime:    &cancelledEndTime,
	}}, nil).Once()
	mockClient.EXPECT().GetJobsByID(mock.Anything, "103").Return([]slurmapi.Job{{
		ID:         103,
		Name:       activeCheck.Name,
		State:      string(api.V0041JobInfoJobStateCOMPLETED),
		SubmitTime: &submitTime,
		EndTime:    &completedEndTime,
	}}, nil).Once()
	mockClient.EXPECT().GetJobsByID(mock.Anything, "104").Return([]slurmapi.Job{{
		ID:          104,
		Name:        activeCheck.Name,
		State:       string(api.V0041JobInfoJobStateTIMEOUT),
		StateReason: "timeout",
		SubmitTime:  &submitTime,
		EndTime:     &errorEndTime,
	}}, nil).Once()

	reconciler, fakeClient := newActiveCheckJobTestReconciler(t, scheme, activeCheck, cronJob, k8sJob, pod, mockClient)

	result, err := reconciler.Reconcile(ctx, newActiveCheckJobRequest(k8sJob))
	require.NoError(t, err)
	assert.False(t, result.Requeue)

	updatedCheck := &slurmv1alpha1.ActiveCheck{}
	require.NoError(t, fakeClient.Get(ctx, client.ObjectKeyFromObject(activeCheck), updatedCheck))
	assert.Equal(t, "101", updatedCheck.Status.SlurmJobsStatus.LastRunId)
	assert.Equal(t, activeCheck.Name, updatedCheck.Status.SlurmJobsStatus.LastRunName)
	assert.Equal(t, consts.ActiveCheckSlurmRunStatusFailed, updatedCheck.Status.SlurmJobsStatus.LastRunStatus)
	require.NotNil(t, updatedCheck.Status.SlurmJobsStatus.LastRunSubmitTime)
	assert.Equal(t, submitTime.Unix(), updatedCheck.Status.SlurmJobsStatus.LastRunSubmitTime.Unix())
	assert.Equal(t, []slurmv1alpha1.JobAndReason{{
		JobID:  "101",
		Reason: "boom",
	}}, updatedCheck.Status.SlurmJobsStatus.LastRunFailJobsAndReasons)
	assert.Equal(t, []slurmv1alpha1.JobAndReason{{
		JobID:  "104",
		Reason: "timeout",
	}}, updatedCheck.Status.SlurmJobsStatus.LastRunErrorJobsAndReasons)
	assert.Equal(t, []string{"102"}, updatedCheck.Status.SlurmJobsStatus.LastRunCancelledJobs)

	updatedJob := &batchv1.Job{}
	require.NoError(t, fakeClient.Get(ctx, client.ObjectKeyFromObject(k8sJob), updatedJob))
	assert.Equal(t, fmt.Sprintf("%d", errorEndTime.Unix()), updatedJob.Annotations[K8sAnnotationSoperatorChecksFinalStateTime])
}

func TestActiveCheckJobReconciler_Reconcile_SlurmJobAccumulatesTerminalResultsAcrossReconciles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := newActiveCheckJobTestScheme(t)

	activeCheck, cronJob, k8sJob, pod := newActiveCheckJobTestObjects("gpu-check", "gpu-check-123", "slurmJob")
	k8sJob.Annotations["slurm-job-id"] = "101,102"

	submitTime := metav1.NewTime(time.Date(2026, time.April, 13, 10, 0, 0, 0, time.UTC))
	failedEndTime := metav1.NewTime(submitTime.Add(1 * time.Minute))
	cancelledEndTime := metav1.NewTime(submitTime.Add(3 * time.Minute))

	mockClient := slurmapifake.NewMockClient(t)
	reconcileRound := 1

	mockClient.EXPECT().
		GetJobsByID(mock.Anything, "101").
		RunAndReturn(func(context.Context, string) ([]slurmapi.Job, error) {
			return []slurmapi.Job{{
				ID:          101,
				Name:        activeCheck.Name,
				State:       string(api.V0041JobInfoJobStateFAILED),
				StateReason: "boom",
				SubmitTime:  &submitTime,
				EndTime:     &failedEndTime,
			}}, nil
		}).
		Twice()

	mockClient.EXPECT().
		GetJobsByID(mock.Anything, "102").
		RunAndReturn(func(context.Context, string) ([]slurmapi.Job, error) {
			if reconcileRound == 1 {
				return []slurmapi.Job{{
					ID:         102,
					Name:       activeCheck.Name,
					State:      "RUNNING",
					SubmitTime: &submitTime,
				}}, nil
			}

			return []slurmapi.Job{{
				ID:         102,
				Name:       activeCheck.Name,
				State:      string(api.V0041JobInfoJobStateCANCELLED),
				SubmitTime: &submitTime,
				EndTime:    &cancelledEndTime,
			}}, nil
		}).
		Twice()

	reconciler, fakeClient := newActiveCheckJobTestReconciler(t, scheme, activeCheck, cronJob, k8sJob, pod, mockClient)
	req := newActiveCheckJobRequest(k8sJob)

	firstResult, err := reconciler.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.True(t, firstResult.Requeue)

	updatedCheck := &slurmv1alpha1.ActiveCheck{}
	require.NoError(t, fakeClient.Get(ctx, client.ObjectKeyFromObject(activeCheck), updatedCheck))
	assert.Equal(t, consts.ActiveCheckSlurmRunStatusInProgress, updatedCheck.Status.SlurmJobsStatus.LastRunStatus)
	assert.Equal(t, []slurmv1alpha1.JobAndReason{{
		JobID:  "101",
		Reason: "boom",
	}}, updatedCheck.Status.SlurmJobsStatus.LastRunFailJobsAndReasons)

	reconcileRound = 2

	secondResult, err := reconciler.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.False(t, secondResult.Requeue)

	require.NoError(t, fakeClient.Get(ctx, client.ObjectKeyFromObject(activeCheck), updatedCheck))
	assert.Equal(t, consts.ActiveCheckSlurmRunStatusFailed, updatedCheck.Status.SlurmJobsStatus.LastRunStatus)
	assert.Equal(t, []slurmv1alpha1.JobAndReason{{
		JobID:  "101",
		Reason: "boom",
	}}, updatedCheck.Status.SlurmJobsStatus.LastRunFailJobsAndReasons)
	assert.Equal(t, []string{"102"}, updatedCheck.Status.SlurmJobsStatus.LastRunCancelledJobs)

	updatedJob := &batchv1.Job{}
	require.NoError(t, fakeClient.Get(ctx, client.ObjectKeyFromObject(k8sJob), updatedJob))
	assert.Equal(t, fmt.Sprintf("%d", cancelledEndTime.Unix()), updatedJob.Annotations[K8sAnnotationSoperatorChecksFinalStateTime])
}

func TestActiveCheckJobReconciler_Reconcile_FailedSlurmJobWithoutReactionsOnlyUpdatesStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := newActiveCheckJobTestScheme(t)

	activeCheck, cronJob, k8sJob, pod := newActiveCheckJobTestObjects("gpu-check", "gpu-check-123", "slurmJob")
	k8sJob.Annotations["slurm-job-id"] = "101"

	submitTime := metav1.NewTime(time.Date(2026, time.April, 13, 10, 0, 0, 0, time.UTC))
	failedEndTime := metav1.NewTime(submitTime.Add(1 * time.Minute))

	mockClient := slurmapifake.NewMockClient(t)
	mockClient.EXPECT().GetJobsByID(mock.Anything, "101").Return([]slurmapi.Job{{
		ID:          101,
		Name:        activeCheck.Name,
		State:       string(api.V0041JobInfoJobStateFAILED),
		StateReason: "boom",
		SubmitTime:  &submitTime,
		EndTime:     &failedEndTime,
		Nodes:       "worker-0",
	}}, nil).Once()

	reconciler, fakeClient := newActiveCheckJobTestReconciler(t, scheme, activeCheck, cronJob, k8sJob, pod, mockClient)

	result, err := reconciler.Reconcile(ctx, newActiveCheckJobRequest(k8sJob))
	require.NoError(t, err)
	assert.False(t, result.Requeue)

	updatedCheck := &slurmv1alpha1.ActiveCheck{}
	require.NoError(t, fakeClient.Get(ctx, client.ObjectKeyFromObject(activeCheck), updatedCheck))
	assert.Equal(t, consts.ActiveCheckSlurmRunStatusFailed, updatedCheck.Status.SlurmJobsStatus.LastRunStatus)
	assert.Equal(t, []slurmv1alpha1.JobAndReason{{
		JobID:  "101",
		Reason: "boom",
	}}, updatedCheck.Status.SlurmJobsStatus.LastRunFailJobsAndReasons)
}

func TestActiveCheckJobReconciler_Reconcile_FailedSlurmJobExecutesCommentReaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := newActiveCheckJobTestScheme(t)

	activeCheck, cronJob, k8sJob, pod := newActiveCheckJobTestObjects("gpu-check", "gpu-check-123", "slurmJob")
	activeCheck.Spec.FailureReactions = &slurmv1alpha1.Reactions{
		CommentSlurmNode: &slurmv1alpha1.CommentSlurmNodeSpec{
			CommentPrefix: "[node_problem]",
		},
	}
	k8sJob.Annotations["slurm-job-id"] = "101"

	submitTime := metav1.NewTime(time.Date(2026, time.April, 13, 10, 0, 0, 0, time.UTC))
	failedEndTime := metav1.NewTime(submitTime.Add(1 * time.Minute))

	mockClient := slurmapifake.NewMockClient(t)
	mockClient.EXPECT().GetJobsByID(mock.Anything, "101").Return([]slurmapi.Job{{
		ID:          101,
		Name:        activeCheck.Name,
		State:       string(api.V0041JobInfoJobStateFAILED),
		StateReason: "boom",
		SubmitTime:  &submitTime,
		EndTime:     &failedEndTime,
		Nodes:       "worker-0",
	}}, nil).Once()
	mockClient.EXPECT().
		SlurmV0041PostNodeWithResponse(
			mock.Anything,
			"worker-0",
			mock.MatchedBy(func(body api.SlurmV0041PostNodeJSONRequestBody) bool {
				expectedComment := "[node_problem] gpu-check: job 101 [slurm_job]"
				return body.Comment != nil && *body.Comment == expectedComment && body.State == nil
			}),
		).
		Return(&api.SlurmV0041PostNodeResponse{
			JSON200: &api.V0041OpenapiResp{
				Errors: &[]api.V0041OpenapiError{},
			},
		}, nil).
		Once()

	reconciler, fakeClient := newActiveCheckJobTestReconciler(t, scheme, activeCheck, cronJob, k8sJob, pod, mockClient)

	result, err := reconciler.Reconcile(ctx, newActiveCheckJobRequest(k8sJob))
	require.NoError(t, err)
	assert.False(t, result.Requeue)

	updatedCheck := &slurmv1alpha1.ActiveCheck{}
	require.NoError(t, fakeClient.Get(ctx, client.ObjectKeyFromObject(activeCheck), updatedCheck))
	assert.Equal(t, consts.ActiveCheckSlurmRunStatusFailed, updatedCheck.Status.SlurmJobsStatus.LastRunStatus)
	assert.Equal(t, []slurmv1alpha1.JobAndReason{{
		JobID:  "101",
		Reason: "boom",
	}}, updatedCheck.Status.SlurmJobsStatus.LastRunFailJobsAndReasons)
}

func TestActiveCheckJobReconciler_Reconcile_K8sJobUpdatesStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := newActiveCheckJobTestScheme(t)

	activeCheck, cronJob, k8sJob, pod := newActiveCheckJobTestObjects("script-check", "script-check-123", "k8sJob")
	lastScheduleTime := metav1.NewTime(time.Date(2026, time.April, 13, 10, 0, 0, 0, time.UTC))
	lastSuccessfulTime := metav1.NewTime(lastScheduleTime.Add(2 * time.Minute))
	cronJob.Status.LastScheduleTime = &lastScheduleTime
	cronJob.Status.LastSuccessfulTime = &lastSuccessfulTime
	k8sJob.Status.Conditions = []batchv1.JobCondition{{
		Type:   batchv1.JobComplete,
		Status: corev1.ConditionTrue,
	}}

	reconciler, fakeClient := newActiveCheckJobTestReconciler(t, scheme, activeCheck, cronJob, k8sJob, pod, nil)

	result, err := reconciler.Reconcile(ctx, newActiveCheckJobRequest(k8sJob))
	require.NoError(t, err)
	assert.False(t, result.Requeue)

	updatedCheck := &slurmv1alpha1.ActiveCheck{}
	require.NoError(t, fakeClient.Get(ctx, client.ObjectKeyFromObject(activeCheck), updatedCheck))
	assert.Equal(t, k8sJob.Name, updatedCheck.Status.K8sJobsStatus.LastJobName)
	assert.Equal(t, consts.ActiveCheckK8sJobStatusComplete, updatedCheck.Status.K8sJobsStatus.LastJobStatus)
	require.NotNil(t, updatedCheck.Status.K8sJobsStatus.LastJobScheduleTime)
	require.NotNil(t, updatedCheck.Status.K8sJobsStatus.LastJobSuccessfulTime)
	assert.Equal(t, lastScheduleTime.Unix(), updatedCheck.Status.K8sJobsStatus.LastJobScheduleTime.Unix())
	assert.Equal(t, lastSuccessfulTime.Unix(), updatedCheck.Status.K8sJobsStatus.LastJobSuccessfulTime.Unix())
	assert.False(t, updatedCheck.Status.K8sJobsStatus.LastTransitionTime.IsZero())
}

func TestActiveCheckJobReconciler_Reconcile_K8sJobNoopWhenStatusDidNotChange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := newActiveCheckJobTestScheme(t)

	activeCheck, cronJob, k8sJob, pod := newActiveCheckJobTestObjects("script-check", "script-check-123", "k8sJob")
	originalTransitionTime := metav1.NewTime(time.Date(2026, time.April, 10, 10, 0, 0, 0, time.UTC))
	activeCheck.Status.K8sJobsStatus = slurmv1alpha1.ActiveCheckK8sJobsStatus{
		LastTransitionTime: originalTransitionTime,
		LastJobName:        k8sJob.Name,
		LastJobStatus:      consts.ActiveCheckK8sJobStatusPending,
	}

	reconciler, fakeClient := newActiveCheckJobTestReconciler(t, scheme, activeCheck, cronJob, k8sJob, pod, nil)

	result, err := reconciler.Reconcile(ctx, newActiveCheckJobRequest(k8sJob))
	require.NoError(t, err)
	assert.False(t, result.Requeue)

	updatedCheck := &slurmv1alpha1.ActiveCheck{}
	require.NoError(t, fakeClient.Get(ctx, client.ObjectKeyFromObject(activeCheck), updatedCheck))
	assert.Equal(t, originalTransitionTime.Unix(), updatedCheck.Status.K8sJobsStatus.LastTransitionTime.Unix())
	assert.Equal(t, k8sJob.Name, updatedCheck.Status.K8sJobsStatus.LastJobName)
	assert.Equal(t, consts.ActiveCheckK8sJobStatusPending, updatedCheck.Status.K8sJobsStatus.LastJobStatus)
	assert.Nil(t, updatedCheck.Status.K8sJobsStatus.LastJobScheduleTime)
	assert.Nil(t, updatedCheck.Status.K8sJobsStatus.LastJobSuccessfulTime)
}

func newActiveCheckJobTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, batchv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))

	return scheme
}

func newActiveCheckJobTestObjects(
	activeCheckName string,
	k8sJobName string,
	checkType string,
) (*slurmv1alpha1.ActiveCheck, *batchv1.CronJob, *batchv1.Job, *corev1.Pod) {
	const namespace = "test-ns"

	activeCheck := &slurmv1alpha1.ActiveCheck{
		ObjectMeta: metav1.ObjectMeta{
			Name:      activeCheckName,
			Namespace: namespace,
		},
		Spec: slurmv1alpha1.ActiveCheckSpec{
			Name:                activeCheckName,
			CheckType:           checkType,
			SlurmClusterRefName: "cluster-a",
		},
	}

	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      activeCheckName,
			Namespace: namespace,
		},
	}

	k8sJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sJobName,
			Namespace: namespace,
			Labels: map[string]string{
				consts.LabelComponentKey: consts.ComponentTypeSoperatorChecks.String(),
			},
			Annotations: map[string]string{},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sJobName + "-pod",
			Namespace: namespace,
			Labels: map[string]string{
				"job-name": k8sJobName,
			},
			Annotations: map[string]string{
				consts.AnnotationActiveCheckName: activeCheckName,
			},
		},
	}

	return activeCheck, cronJob, k8sJob, pod
}

func newActiveCheckJobTestReconciler(
	t *testing.T,
	scheme *runtime.Scheme,
	activeCheck *slurmv1alpha1.ActiveCheck,
	cronJob *batchv1.CronJob,
	k8sJob *batchv1.Job,
	pod *corev1.Pod,
	slurmClient slurmapi.Client,
) (*ActiveCheckJobReconciler, client.Client) {
	t.Helper()

	fakeClient := clientfake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(activeCheck).
		WithObjects(activeCheck, cronJob, k8sJob, pod).
		Build()

	slurmClients := slurmapi.NewClientSet(context.Background())
	if slurmClient != nil {
		slurmClients.AddClient(types.NamespacedName{
			Namespace: activeCheck.Namespace,
			Name:      activeCheck.Spec.SlurmClusterRefName,
		}, slurmClient)
	}

	return NewActiveCheckJobController(
		fakeClient,
		scheme,
		record.NewFakeRecorder(10),
		slurmClients,
		time.Minute,
	), fakeClient
}

func newActiveCheckJobRequest(k8sJob *batchv1.Job) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: k8sJob.Namespace,
		Name:      k8sJob.Name,
	}}
}
