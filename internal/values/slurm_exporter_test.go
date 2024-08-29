package values

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

func Test_BuildSlurmExporterFrom(t *testing.T) {

	clusterName := "testCluster"
	telemetry := &slurmv1.Telemetry{
		Prometheus: &slurmv1.MetricsPrometheus{},
	}
	controller := &slurmv1.SlurmNodeController{
		SlurmNode: slurmv1.SlurmNode{},
		Munge: slurmv1.NodeContainer{
			Image: "testImage",
		},
	}

	result := buildSlurmExporterFrom(clusterName, telemetry, controller)

	assert.NotNil(t, result.MetricsPrometheus)
	assert.NotNil(t, result.SlurmNode)
	assert.NotNil(t, result.ContainerMunge)
	assert.Equal(t, fmt.Sprintf("%s-exporter", clusterName), result.Name)
}

func Test_BuildSlurmExporterFromWithNilTelemetry(t *testing.T) {
	clusterName := "testCluster"
	telemetry := (*slurmv1.Telemetry)(nil)
	controller := &slurmv1.SlurmNodeController{
		SlurmNode: slurmv1.SlurmNode{},
		Munge: slurmv1.NodeContainer{
			Image: "testImage",
		},
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("The code panicked: %v", r)
		}
	}()
	buildSlurmExporterFrom(clusterName, telemetry, controller)
}

func Test_BuildSlurmExporterFromWithNilPrometheus(t *testing.T) {
	clusterName := "testCluster"
	telemetry := &slurmv1.Telemetry{
		Prometheus: nil,
	}
	controller := &slurmv1.SlurmNodeController{
		SlurmNode: slurmv1.SlurmNode{},
		Munge: slurmv1.NodeContainer{
			Image: "testImage",
		},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("The code panicked: %v", r)
		}
	}()
	buildSlurmExporterFrom(clusterName, telemetry, controller)
}
