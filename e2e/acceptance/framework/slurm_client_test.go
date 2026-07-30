package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSacctJob(t *testing.T) {
	dump := `
123.batch|COMPLETED|0:0||
123|FAILED|1:0|Prolog failure|
`

	state, exitCode, reason, found := parseSacctJob(dump, "123")

	assert.True(t, found)
	assert.Equal(t, "FAILED", state)
	assert.Equal(t, "1:0", exitCode)
	assert.Equal(t, "Prolog failure", reason)
}

func TestSlurmJobInfoCompletedSuccessfully(t *testing.T) {
	info := SlurmJobInfo{
		SacctFound: true,
		SacctState: "COMPLETED",
		SacctExit:  "0:0",
	}

	assert.True(t, info.CompletedSuccessfully())
	assert.False(t, info.IsAlive())
}
