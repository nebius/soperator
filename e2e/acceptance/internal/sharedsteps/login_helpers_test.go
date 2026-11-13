package sharedsteps

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoginFeatureEnabled(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		enabled bool
	}{
		{"", false}, {"null", false}, {" null ", false}, {"{}", false},
		{`{"enabled":false}`, false}, {`{"enabled":true,"other":42}`, true},
	} {
		enabled, err := loginFeatureEnabled(json.RawMessage(tc.raw))
		require.NoError(t, err)
		assert.Equal(t, tc.enabled, enabled)
	}
	for _, raw := range []string{`{`, `{"enabled":"yes"}`, `[]`} {
		_, err := loginFeatureEnabled(json.RawMessage(raw))
		require.Error(t, err)
	}
}
