package values

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

func Test_BuildSlurmExporterFrom(t *testing.T) {

	clusterName := "testCluster"
	exporter := &slurmv1.SlurmExporter{
		SlurmNode: slurmv1.SlurmNode{},
		Exporter:  slurmv1.ExporterContainer{},
		Munge: slurmv1.NodeContainer{
			Image: "testImage",
		},
	}

	result := buildSlurmExporterFrom(clusterName, exporter)

	assert.NotNil(t, result.ExporterContainer)
	assert.NotNil(t, result.SlurmNode)
	assert.NotNil(t, result.ContainerMunge)
	assert.Equal(t, fmt.Sprintf("%s-exporter", clusterName), result.Name)
}

func Test_BuildSlurmExporterFromWithNilTelemetry(t *testing.T) {
	clusterName := "testCluster"
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
	buildSlurmExporterFrom(clusterName, exporter)
}

func Test_BuildSlurmExporterFromWithNilPrometheus(t *testing.T) {
	clusterName := "testCluster"
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
	buildSlurmExporterFrom(clusterName, exporter)
}
