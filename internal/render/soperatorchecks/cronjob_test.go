package soperatorchecks_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/soperatorchecks"
)

func Test_RenderK8sCronJob_ClusterExtraLabelsAndAnnotations(t *testing.T) {
	check := &slurmv1alpha1.ActiveCheck{
		ObjectMeta: metav1.ObjectMeta{Name: "test-check", Namespace: "test-ns"},
		Spec: slurmv1alpha1.ActiveCheckSpec{
			Name:                "test-check",
			SlurmClusterRefName: "test-cluster",
			CheckType:           "k8sJob",
		},
	}

	extraLabels := map[string]string{
		"gcore.com/project-id":  "123",
		consts.LabelComponentKey: "hijacked",
	}
	extraAnnotations := map[string]string{"gcore.com/note": "abc"}

	cronJob, err := soperatorchecks.RenderK8sCronJob(check, nil, extraLabels, extraAnnotations)
	require.NoError(t, err)

	assert.Equal(t, "123", cronJob.Labels["gcore.com/project-id"])
	assert.Equal(t, consts.ComponentTypeSoperatorChecks.String(), cronJob.Labels[consts.LabelComponentKey])

	podLabels := cronJob.Spec.JobTemplate.Spec.Template.Labels
	assert.Equal(t, "123", podLabels["gcore.com/project-id"])
	assert.Equal(t, consts.ComponentTypeSoperatorChecks.String(), podLabels[consts.LabelComponentKey])

	podAnnotations := cronJob.Spec.JobTemplate.Spec.Template.Annotations
	assert.Equal(t, "abc", podAnnotations["gcore.com/note"])
}
