package values

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

func Test_BuildSlurmExporterFrom(t *testing.T) {
	exporter := &slurmv1.SlurmExporter{
		SlurmNode: slurmv1.SlurmNode{},
		Exporter:  slurmv1.ExporterContainer{},
		Munge: slurmv1.NodeContainer{
			Image: "testImage",
		},
		JobSource:              "accounting",
		AccountingJobStates:    []string{"RUNNING", "PENDING"},
		AccountingJobsLookback: prometheusv1.Duration("30m"),
	}

	result := buildSlurmExporterFrom(ptr.To(consts.ModeNone), exporter)

	assert.NotNil(t, result.ExporterContainer)
	assert.NotNil(t, result.SlurmNode)
	assert.NotNil(t, result.ContainerMunge)
	assert.Equal(t, "accounting", result.JobSource)
	assert.Equal(t, []string{"RUNNING", "PENDING"}, result.AccountingJobStates)
	assert.Equal(t, prometheusv1.Duration("30m"), result.AccountingJobsLookback)
}

func Test_BuildSlurmExporterFromWithNilTelemetry(t *testing.T) {
	exporter := &slurmv1.SlurmExporter{
		SlurmNode: slurmv1.SlurmNode{},
		Exporter:  slurmv1.ExporterContainer{},
		Munge: slurmv1.NodeContainer{
			Image: "testImage",
		},
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("The code panicked: %v", r)
		}
	}()
	buildSlurmExporterFrom(ptr.To(consts.ModeNone), exporter)
}

func Test_BuildSlurmExporterFromWithNilPrometheus(t *testing.T) {
	exporter := &slurmv1.SlurmExporter{
		SlurmNode: slurmv1.SlurmNode{},
		Exporter:  slurmv1.ExporterContainer{},
		Munge: slurmv1.NodeContainer{
			Image: "testImage",
		},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("The code panicked: %v", r)
		}
	}()
	buildSlurmExporterFrom(ptr.To(consts.ModeNone), exporter)
}
