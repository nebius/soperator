package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestValidateExpectedPrologFailure(t *testing.T) {
	require.NoError(t, validateExpectedPrologFailure("17", "alloc_mem_used", framework.SlurmJobInfo{
		SacctFound: true,
		SacctState: "FAILED",
		SacctExit:  "1:0",
	}))

	tests := []struct {
		name string
		info framework.SlurmJobInfo
		want string
	}{
		{name: "missing sacct", info: framework.SlurmJobInfo{SacctDump: "no rows"}, want: "recorded in sacct"},
		{name: "successful", info: framework.SlurmJobInfo{SacctFound: true, SacctState: "COMPLETED", SacctExit: "0:0"}, want: "fail in Prolog due to alloc_mem_used"},
		{name: "still alive", info: framework.SlurmJobInfo{SacctFound: true, SacctState: "COMPLETING", SacctExit: "0:0"}, want: "finished after leaving squeue"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExpectedPrologFailure("17", "alloc_mem_used", tt.info)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}
