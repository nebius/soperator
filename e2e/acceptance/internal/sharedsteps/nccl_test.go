package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestNCCLTestCommand(t *testing.T) {
	command := ncclTestCommand([]framework.WorkerInfo{{Name: "worker-0"}, {Name: "worker-1"}})

	assert.Contains(t, command, "timeout --kill-after=30s 360")
	assert.Contains(t, command, "'soperator-nccl-test'")
	assert.Contains(t, command, "--nodes=2")
	assert.Contains(t, command, "--nodelist='worker-0,worker-1'")
	assert.Contains(t, command, "--immediate=120")
	assert.Contains(t, command, "--min-message-size=512M")
	assert.Contains(t, command, "--max-message-size=1G")
	assert.Contains(t, command, "--time-limit=3:00")
	assert.NotContains(t, command, "--partition")
	assert.Contains(t, command, "2>&1")
}

func TestParseNCCLResult(t *testing.T) {
	for _, platform := range []string{"H100", "H200", "B200", "B300", "GB300"} {
		t.Run(platform, func(t *testing.T) {
			result, err := parseNCCLResult("Detected platform: " + platform + `
# Out of bounds values : 0 OK
# Avg bus bandwidth    : 468.356
`)

			require.NoError(t, err)
			assert.Equal(t, platform, result.Platform)
			assert.Equal(t, 468.356, result.AvgBandwidth)
		})
	}
}

func TestParseNCCLResultRejectsInvalidOutput(t *testing.T) {
	for name, output := range map[string]string{
		"unsupported platform": `Detected platform: V100
# Out of bounds values : 0 OK
# Avg bus bandwidth : 10
`,
		"incorrect result": `Detected platform: B300
# Out of bounds values : 1 FAILED
# Avg bus bandwidth : 10
`,
		"zero bandwidth": `Detected platform: B300
# Out of bounds values : 0 OK
# Avg bus bandwidth : 0
`,
		"duplicate summary": `Detected platform: B300
# Out of bounds values : 0 OK
# Out of bounds values : 0 OK
# Avg bus bandwidth : 10
`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseNCCLResult(output)
			assert.Error(t, err)
		})
	}
}
