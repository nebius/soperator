package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBashLC(t *testing.T) {
	assert.Equal(t, `bash -lc 'echo '"'"'ready'"'"''`, BashLC(`echo 'ready'`))
}
