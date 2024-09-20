package values

import (
	"testing"

	"github.com/stretchr/testify/assert"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

func Test_BuildSlurmExporterFrom(t *testing.T) {

	exporter := &slurmv1.SlurmExporter{
		SlurmNode: slurmv1.SlurmNode{},
		Exporter:  slurmv1.ExporterContainer{},
		Munge: slurmv1.NodeContainer{
			Image: "testImage",
		},
	}

	result := buildSlurmExporterFrom(exporter)

	assert.NotNil(t, result.ExporterContainer)
	assert.NotNil(t, result.SlurmNode)
	assert.NotNil(t, result.ContainerMunge)
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
	buildSlurmExporterFrom(exporter)
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
	buildSlurmExporterFrom(exporter)
}
