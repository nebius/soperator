package nodeconfigurator

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

func TestRenderDaemonSetIncludesRollingUpdateStrategy(t *testing.T) {
	nodeConfigurator := &slurmv1alpha1.NodeConfigurator{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-configurator",
		},
		Spec: slurmv1alpha1.NodeConfiguratorSpec{
			Rebooter: slurmv1alpha1.Rebooter{
				Enabled: true,
			},
		},
	}

	daemonSet := RenderDaemonSet(nodeConfigurator, "test-namespace")
	if daemonSet == nil {
		t.Fatal("expected daemonset to be rendered")
	}

	if daemonSet.Spec.UpdateStrategy.Type != appsv1.RollingUpdateDaemonSetStrategyType {
		t.Fatalf("UpdateStrategy.Type = %v, want %v", daemonSet.Spec.UpdateStrategy.Type, appsv1.RollingUpdateDaemonSetStrategyType)
	}

	if daemonSet.Spec.UpdateStrategy.RollingUpdate == nil {
		t.Fatal("expected RollingUpdate strategy to be set")
	}

	if daemonSet.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable == nil {
		t.Fatal("expected MaxUnavailable to be set")
	}

	if daemonSet.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable.String() != "20%" {
		t.Fatalf("MaxUnavailable = %s, want 20%%", daemonSet.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable.String())
	}
}
