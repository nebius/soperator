package reports

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormat(t *testing.T) {
	format, err := Format("", "soperator")
	require.NoError(t, err)
	assert.Equal(t, "pretty", format)

	dir := t.TempDir()
	format, err = Format(dir, "soperator")
	require.NoError(t, err)
	assert.Equal(t,
		"pretty,cucumber:"+filepath.Join(dir, "soperator.cucumber.json")+
			",junit:"+filepath.Join(dir, "soperator.junit.xml"),
		format,
	)
}
