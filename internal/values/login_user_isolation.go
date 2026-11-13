package values

import (
	"k8s.io/apimachinery/pkg/api/resource"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

const (
	loginUserIsolationMemoryHighPercent int64 = 40
	loginUserIsolationMemoryMaxPercent  int64 = 50
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

const (
	loginUserIsolationCPUPeriodDefaultUS int64 = 100000

	// LoginUserIsolationCPUQuotaMinUS is the smallest cpu.max quota accepted by the kernel.
	LoginUserIsolationCPUQuotaMinUS int64 = 1000
)

// ResolveLoginUserIsolationCPUMax returns the cpu.max quota and period in microseconds.
// The quota is nil when the per-user CPU usage is not capped.
func ResolveLoginUserIsolationCPUMax(isolation *slurmv1.LoginUserIsolation) (quotaUS *int64, periodUS int64) {
	periodUS = loginUserIsolationCPUPeriodDefaultUS
	if isolation == nil {
		return nil, periodUS
	}
	if isolation.CPUPeriod != 0 {
		// The CRD defaults cpuPeriod on admission; guard for objects built in code.
		periodUS = isolation.CPUPeriod
	}
	if isolation.CPUQuota == nil {
		return nil, periodUS
	}

	quota := isolation.CPUQuota.MilliValue() * periodUS / 1000
	return &quota, periodUS
}
