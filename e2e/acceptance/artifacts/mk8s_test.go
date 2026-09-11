package artifacts

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestMK8sCollectorSkipsWithoutProjectID(t *testing.T) {
	local := framework.NewArgsScope(func(context.Context, ...string) (string, error) {
		t.Fatal("Nebius CLI must not be invoked without a project ID")
		return "", nil
	})

	err := NewMK8sCollector(local, " ").Collect(t.Context(), t.TempDir())
	require.NoError(t, err)
}
