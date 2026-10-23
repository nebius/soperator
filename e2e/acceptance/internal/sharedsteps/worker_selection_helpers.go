package sharedsteps

import (
	"fmt"
	"strings"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func pickWorkersByType(snapshot framework.WorkerSnapshot, workerType string, count int) ([]framework.WorkerInfo, error) {
	switch strings.ToUpper(workerType) {
	case "CPU":
		return snapshot.PickCPUWorkers(count)
	case "GPU":
		return snapshot.PickGPUWorkers(count)
	default:
		return nil, fmt.Errorf("unsupported worker type %q", workerType)
	}
}
