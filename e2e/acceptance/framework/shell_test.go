package framework

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellJoinRoundTripsThroughBash(t *testing.T) {
	words := []string{"printf", `%s\n`, "plain", "with space", "it's quoted", "$(not expanded)", "--flag=a:b", ""}
	command := ShellJoin(words...)
	assert.Equal(t, `'printf' '%s\n' 'plain' 'with space' 'it'"'"'s quoted' '$(not expanded)' '--flag=a:b' ''`, command)

	output, err := exec.CommandContext(t.Context(), "bash", "-c", command).CombinedOutput()
	require.NoError(t, err, string(output))
	assert.Equal(t, words[2:], strings.Split(strings.TrimSuffix(string(output), "\n"), "\n"))
}

func TestShellJoinEmpty(t *testing.T) {
	assert.Equal(t, "", ShellJoin())
}
