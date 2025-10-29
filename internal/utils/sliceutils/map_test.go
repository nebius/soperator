package sliceutils_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"nebius.ai/slurm-operator/internal/utils/sliceutils"
)

func TestMap(t *testing.T) {
	t.Run("Test Map string", func(t *testing.T) {
		f := sliceutils.Map(testCases, func(t TestCase) string {
			return fmt.Sprintf("%d x %s", t.A, t.B)
		})
		assert.Equal(t, "10 x hello", f[0])
		assert.Equal(t, "20 x bye", f[1])
	})

	t.Run("Test Map custom type", func(t *testing.T) {
		type custom struct {
			Foo int
			Bar string
		}
		f := sliceutils.Map(testCases, func(t TestCase) custom {
			return custom{
				Foo: t.A * 2,
				Bar: t.B + t.B,
			}
		})
		assert.Equal(t, 20, f[0].Foo)
		assert.Equal(t, 40, f[1].Foo)
		assert.Equal(t, "hellohello", f[0].Bar)
		assert.Equal(t, "byebye", f[1].Bar)
	})
}
