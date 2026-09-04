package values

import (
	"k8s.io/apimachinery/pkg/api/resource"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

const (
	loginUserIsolationMemoryHighPercent int64 = 80
	loginUserIsolationMemoryMaxPercent  int64 = 90
)

// ResolveLoginUserIsolationMemoryLimits returns the effective limits after applying derived defaults.
func ResolveLoginUserIsolationMemoryLimits(
	isolation *slurmv1.LoginUserIsolation,
	containerMemory *resource.Quantity,
) (memoryHigh, memoryMax *resource.Quantity) {
	if isolation == nil {
		return nil, nil
	}

	memoryHigh = isolation.MemoryHigh
	if memoryHigh == nil {
		memoryHigh = deriveLoginUserIsolationMemory(containerMemory, loginUserIsolationMemoryHighPercent)
	}

	memoryMax = isolation.MemoryMax
	if memoryMax == nil {
		memoryMax = deriveLoginUserIsolationMemory(containerMemory, loginUserIsolationMemoryMaxPercent)
	}

	return memoryHigh, memoryMax
}

func deriveLoginUserIsolationMemory(containerMemory *resource.Quantity, percent int64) *resource.Quantity {
	if containerMemory == nil || containerMemory.Sign() <= 0 {
		return nil
	}

	return resource.NewQuantity(containerMemory.Value()*percent/100, resource.BinarySI)
}
