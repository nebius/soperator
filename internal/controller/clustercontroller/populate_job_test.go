package clustercontroller

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/values"
)

func newTestReconciler(t *testing.T, objects ...client.Object) SlurmClusterReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add batchv1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 to scheme: %v", err)
	}
	if err := slurmv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add slurmv1 to scheme: %v", err)
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()

	return SlurmClusterReconciler{
		Reconciler: reconciler.NewReconciler(fakeClient, scheme, nil),
	}
}

func TestReconcilePopulateJail_ExistingJob(t *testing.T) {
	const (
		namespace = "test-ns"
		jobName   = "test-cluster-populate-jail"
	)

	clusterValues := &values.SlurmCluster{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      "test-cluster",
		},
		PopulateJail: values.PopulateJail{
			Name: jobName,
		},
	}
	cluster := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: namespace,
		},
	}

	tests := []struct {
		name           string
		jobSucceeded   int32
		expectNotReady bool
		expectRequeue  time.Duration
	}{
		{
			name:           "completed job proceeds normally",
			jobSucceeded:   1,
			expectNotReady: false,
			expectRequeue:  0,
		},
		{
			name:           "incomplete job returns notReady",
			jobSucceeded:   0,
			expectNotReady: true,
			expectRequeue:  10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      jobName,
					Namespace: namespace,
				},
				Status: batchv1.JobStatus{
					Succeeded: tt.jobSucceeded,
				},
			}

			r := newTestReconciler(t, job)
			res, notReady, err := r.ReconcilePopulateJail(context.Background(), clusterValues, cluster)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if notReady != tt.expectNotReady {
				t.Errorf("notReady = %v, want %v", notReady, tt.expectNotReady)
			}
			if res.RequeueAfter != tt.expectRequeue {
				t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, tt.expectRequeue)
			}
		})
	}
}

// TestReconcilePopulateJail_MaintenanceOverwriteWithActivePods verifies PR #2183
// logic: in maintenance overwrite mode with active login pods and no Job,
// notReady must be false (so ReconcileLogin/Worker run to scale pods down)
// while RequeueAfter is set (to retry populate-jail creation after scale-down).
func TestReconcilePopulateJail_MaintenanceOverwriteWithActivePods(t *testing.T) {
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
		jobName     = "test-cluster-populate-jail"
	)

	maintenanceMode := consts.ModeDownscaleAndOverwritePopulate
	clusterValues := &values.SlurmCluster{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      clusterName,
		},
		PopulateJail: values.PopulateJail{
			Name:        jobName,
			Maintenance: &maintenanceMode,
		},
	}
	cluster := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
	}

	loginPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "login-0",
			Namespace: namespace,
			Labels: map[string]string{
				consts.LabelInstanceKey:  clusterName,
				consts.LabelComponentKey: consts.ComponentTypeLogin.String(),
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	r := newTestReconciler(t, loginPod)
	res, notReady, err := r.ReconcilePopulateJail(context.Background(), clusterValues, cluster)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if notReady {
		t.Errorf("notReady = true, want false (rest of reconciliation must run to scale down pods)")
	}
	if res.RequeueAfter != 10*time.Second {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, 10*time.Second)
	}
}
