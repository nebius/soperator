package acceptance

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKubectlArgsAddsContext(t *testing.T) {
	assert.Equal(t,
		[]string{"--context", "dev-context", "get", "pods"},
		kubectlArgs("dev-context", []string{"get", "pods"}),
	)
}

func TestKubectlArgsWithoutContext(t *testing.T) {
	assert.Equal(t,
		[]string{"get", "pods"},
		kubectlArgs("", []string{"get", "pods"}),
	)
}
