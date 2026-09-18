package clustercontroller

import (
	"context"
	"errors"
	"testing"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"github.com/stretchr/testify/require"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	renderlogin "nebius.ai/slurm-operator/internal/render/login"
	"nebius.ai/slurm-operator/internal/values"
)

func TestReconcileLoginAutoscalingReplicas(t *testing.T) {
	tests := []struct {
		testName            string
		currentReplicas     *int32
		maintenanceEnabled  bool
		autoscalingDisabled bool
		autoscalingOmitted  bool
		expectedReplicas    int32
		expectedHPAExists   bool
	}{
		{testName: "create at minimum", expectedReplicas: 2, expectedHPAExists: true},
		{testName: "resume from zero", currentReplicas: ptr.To(int32(0)), expectedReplicas: 2, expectedHPAExists: true},
		{testName: "preserve HPA scale", currentReplicas: ptr.To(int32(5)), expectedReplicas: 5, expectedHPAExists: true},
		{testName: "leave positive count below minimum to HPA", currentReplicas: ptr.To(int32(1)), expectedReplicas: 1, expectedHPAExists: true},
		{testName: "maintenance overrides autoscaling", currentReplicas: ptr.To(int32(5)), maintenanceEnabled: true, expectedReplicas: 0, expectedHPAExists: false},
		{testName: "disabled uses configured count", currentReplicas: ptr.To(int32(5)), autoscalingDisabled: true, expectedReplicas: 3, expectedHPAExists: false},
		{testName: "omitted uses configured count", currentReplicas: ptr.To(int32(5)), autoscalingOmitted: true, expectedReplicas: 3, expectedHPAExists: false},
		{testName: "maintenance overrides fixed count", currentReplicas: ptr.To(int32(3)), autoscalingDisabled: true, maintenanceEnabled: true, expectedReplicas: 0, expectedHPAExists: false},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			r, cluster, clusterValues := newLoginTestReconciler(t)
			if tt.maintenanceEnabled {
				clusterValues.NodeLogin.Maintenance = ptr.To(consts.ModeDownscale)
			}
			clusterValues.NodeLogin.Autoscaling.Enabled = !tt.autoscalingDisabled
			if tt.autoscalingOmitted {
				clusterValues.NodeLogin.Autoscaling = nil
			}
			if tt.currentReplicas != nil {
				current, err := renderlogin.RenderStatefulSet(
					clusterValues.Namespace, clusterValues.Name, clusterValues.ClusterWithGPU,
					clusterValues.NodeFilters, &clusterValues.Secrets, clusterValues.VolumeSources,
					&clusterValues.NodeLogin, tt.currentReplicas,
				)
				require.NoError(t, err)
				require.NoError(t, r.Create(context.Background(), &current))
			}
			reconcileLoginAndCheckReplicas(t, r, cluster, clusterValues, tt.expectedReplicas, tt.expectedHPAExists)
		})
	}
}

func TestReconcileLoginAutoscalingMaintenanceRecovery(t *testing.T) {
	r, cluster, clusterValues := newLoginTestReconciler(t)
	// Start at minReplicas with an HPA managing the login StatefulSet.
	reconcileLoginAndCheckReplicas(t, r, cluster, clusterValues, 2, true)

	// Simulate HPA scale-up and verify reconciliation preserves its replica count.
	current := &kruisev1b1.StatefulSet{}
	key := types.NamespacedName{Namespace: clusterValues.Namespace, Name: clusterValues.NodeLogin.StatefulSet.Name}
	require.NoError(t, r.Get(context.Background(), key, current))
	current.Spec.Replicas = ptr.To(int32(5))
	require.NoError(t, r.Update(context.Background(), current))
	reconcileLoginAndCheckReplicas(t, r, cluster, clusterValues, 5, true)

	// Enter maintenance: remove the HPA and keep replicas at zero across reconciliations.
	clusterValues.NodeLogin.Maintenance = ptr.To(consts.ModeDownscale)
	reconcileLoginAndCheckReplicas(t, r, cluster, clusterValues, 0, false)
	reconcileLoginAndCheckReplicas(t, r, cluster, clusterValues, 0, false)

	// Leave maintenance: restore minReplicas and recreate the HPA, then verify the result is stable.
	clusterValues.NodeLogin.Maintenance = nil
	reconcileLoginAndCheckReplicas(t, r, cluster, clusterValues, 2, true)
	reconcileLoginAndCheckReplicas(t, r, cluster, clusterValues, 2, true)

	// Disable autoscaling: remove the HPA and restore the configured fixed replica count.
	clusterValues.NodeLogin.Autoscaling.Enabled = false
	reconcileLoginAndCheckReplicas(t, r, cluster, clusterValues, 3, false)
}

func TestReconcileLoginAutoscalingStatefulSetReadError(t *testing.T) {
	r, cluster, clusterValues := newLoginTestReconciler(t)
	readErr := errors.New("statefulset read unavailable")
	r.Client = loginStatefulSetReadErrorClient{Client: r.Client, err: readErr}
	err := r.ReconcileLogin(context.Background(), cluster, clusterValues)
	require.ErrorContains(t, err, "get login StatefulSet: "+readErr.Error())
}

type loginStatefulSetReadErrorClient struct {
	client.Client
	err error
}

func (c loginStatefulSetReadErrorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*kruisev1b1.StatefulSet); ok {
		return c.err
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func reconcileLoginAndCheckReplicas(t *testing.T, r *SlurmClusterReconciler, cluster *slurmv1.SlurmCluster, clusterValues *values.SlurmCluster, want int32, wantHPA bool) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, r.ReconcileLogin(ctx, cluster, clusterValues))
	key := types.NamespacedName{Namespace: clusterValues.Namespace, Name: clusterValues.NodeLogin.StatefulSet.Name}
	current := &kruisev1b1.StatefulSet{}
	require.NoError(t, r.Get(ctx, key, current))
	require.NotNil(t, current.Spec.Replicas)
	require.Equal(t, want, *current.Spec.Replicas)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	err := r.Get(ctx, key, hpa)
	if wantHPA {
		require.NoError(t, err)
		require.Equal(t, clusterValues.NodeLogin.Autoscaling.MinReplicas, *hpa.Spec.MinReplicas)
	} else {
		require.True(t, apierrors.IsNotFound(err), "expected no HPA, got: %v", err)
	}
}

func newLoginTestReconciler(t *testing.T) (*SlurmClusterReconciler, *slurmv1.SlurmCluster, *values.SlurmCluster) {
	t.Helper()
	const namespace, clusterName = "test-ns", "test-cluster"
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, autoscalingv2.AddToScheme(scheme))
	require.NoError(t, kruisev1b1.AddToScheme(scheme))
	require.NoError(t, slurmv1.AddToScheme(scheme))
	cluster := &slurmv1.SlurmCluster{ObjectMeta: metav1.ObjectMeta{
		Namespace: namespace, Name: clusterName, UID: "test-cluster-uid",
	}}
	sshdConfigName := naming.BuildConfigMapSSHDConfigsNameLogin(clusterName)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "sshd-keys"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: naming.BuildSecretMungeKeyName(clusterName)}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: sshdConfigName}},
	).Build()
	clusterValues := &values.SlurmCluster{
		NamespacedName: types.NamespacedName{Namespace: namespace, Name: clusterName},
		NodeFilters:    []slurmv1.K8sNodeFilter{{Name: "login"}},
		Secrets:        slurmv1.Secrets{SshdKeysName: "sshd-keys"},
		NodeLogin: values.SlurmLogin{
			SlurmNode: slurmv1.SlurmNode{K8sNodeFilterName: "login"},
			StatefulSet: values.StatefulSet{
				Name: naming.BuildStatefulSetName(consts.ComponentTypeLogin, clusterName), Replicas: 3,
			},
			Service:         values.Service{Name: "login-svc"},
			HeadlessService: values.Service{Name: "login-headless-svc"},
			VolumeJail:      slurmv1.NodeVolume{VolumeClaimTemplateSpec: &corev1.PersistentVolumeClaimSpec{}},
			Autoscaling: &slurmv1.LoginAutoscaling{
				Enabled: true, MinReplicas: 2, MaxReplicas: 6, TargetCPUUtilizationPercentage: 70,
			},
		},
	}
	return NewSlurmClusterReconciler(fakeClient, scheme, nil), cluster, clusterValues
}
