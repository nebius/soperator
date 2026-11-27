package values

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

func TestResolveLoginUserIsolationCPUMax(t *testing.T) {
	quantity := func(s string) *resource.Quantity {
		q := resource.MustParse(s)
		return &q
	}

	tests := []struct {
		name       string
		isolation  *slurmv1.LoginUserIsolation
		wantQuota  *int64
		wantPeriod int64
	}{
		{
			name:       "nil isolation",
			wantPeriod: 100000,
		},
		{
			name:       "quota unset",
			isolation:  &slurmv1.LoginUserIsolation{CPUPeriod: 50000},
			wantPeriod: 50000,
		},
		{
			name:       "whole CPUs with default period",
			isolation:  &slurmv1.LoginUserIsolation{CPUQuota: quantity("8")},
			wantQuota:  ptr.To[int64](800000),
			wantPeriod: 100000,
		},
		{
			name:       "fractional CPUs with custom period",
			isolation:  &slurmv1.LoginUserIsolation{CPUQuota: quantity("2500m"), CPUPeriod: 50000},
			wantQuota:  ptr.To[int64](125000),
			wantPeriod: 50000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quota, period := ResolveLoginUserIsolationCPUMax(tt.isolation)
			assert.Equal(t, tt.wantQuota, quota)
			assert.Equal(t, tt.wantPeriod, period)
		})
	}
}
