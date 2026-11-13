package framework

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReservationPowerTimeoutUsesConfigurationParser(t *testing.T) {
	for _, test := range []struct {
		name, output, flag string
		want               time.Duration
		wantError          string
	}{
		{name: "resume", output: "ResumeTimeout = 1800 sec\nSuspendTimeout = 90 sec", flag: "POWERING_UP", want: 31 * time.Minute},
		{name: "suspend", output: "ResumeTimeout = 1800 sec\nSuspendTimeout = 90 sec", flag: "POWERING_DOWN", want: 150 * time.Second},
		{name: "duplicate setting", output: "ResumeTimeout = 1800 sec\nResumeTimeout = 1 sec", flag: "POWERING_UP", wantError: "duplicated"},
		{name: "empty setting name", output: " = 1800 sec\nResumeTimeout = 1 sec", flag: "POWERING_UP", wantError: "setting name is empty"},
		{name: "missing timeout", output: "ResumeTimeout = 1800 sec", flag: "POWERING_DOWN", wantError: "SuspendTimeout"},
		{name: "zero timeout", output: "ResumeTimeout = 0 sec", flag: "POWERING_UP", wantError: "parse configured"},
		{name: "wrong units", output: "ResumeTimeout = 1800 minutes", flag: "POWERING_UP", wantError: "parse configured"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := newSlurmSuspensionRuntime(t, slurmSuspensionReply{command: "scontrol show config", output: test.output})
			backend := &slurmReservations{runtime: runtime}
			timeout, err := backend.powerTimeout(t.Context(), test.flag)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				var invalid SlurmConfigurationError
				require.ErrorAs(t, err, &invalid)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, timeout)
		})
	}
}

func TestParseSlurmSeconds(t *testing.T) {
	value, err := ParseSlurmSeconds(" 0 sec ")
	require.NoError(t, err)
	assert.Zero(t, value)
	for _, raw := range []string{"", "-1 sec", "1800 minutes", "1800", "4294967296 sec"} {
		_, err := ParseSlurmSeconds(raw)
		require.Error(t, err, raw)
	}
}
