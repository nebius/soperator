package sliceutils_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"nebius.ai/slurm-operator/internal/utils/sliceutils"
)

func TestMapDeprecated(t *testing.T) {
	t.Run("Test Map empty", func(t *testing.T) {
		f := sliceutils.Map(emptyTestCases, func(t TestCase) string {
			return fmt.Sprintf("%d x %s", t.A, t.B)
		})
		assert.Empty(t, f)
	})

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

func TestMap(t *testing.T) {
	for _, suite := range []struct {
		Name string
		Impl func([]TestCase, func(testCase TestCase) string) []string
	}{
		{
			Name: "MapSlice",
			Impl: func(xs []TestCase, mapper func(suite TestCase) string) []string {
				return sliceutils.MapSlice(xs, mapper)
			},
		},
		{
			Name: "MapSliceSeq",
			Impl: func(xs []TestCase, mapper func(suite TestCase) string) []string {
				return sliceutils.Collect(sliceutils.MapSliceSeq(xs, mapper))
			},
		},
		{
			Name: "MapSeqSlice",
			Impl: func(xs []TestCase, mapper func(suite TestCase) string) []string {
				return sliceutils.MapSeqSlice(sliceutils.SliceSeq(xs), mapper)
			},
		},
		{
			Name: "MapSeq",
			Impl: func(xs []TestCase, mapper func(suite TestCase) string) []string {
				return sliceutils.Collect(sliceutils.MapSeq(sliceutils.SliceSeq(xs), mapper))
			},
		},
	} {
		t.Run(fmt.Sprintf("Test %s empty", suite.Name), func(t *testing.T) {
			f := suite.Impl(emptyTestCases, func(t TestCase) string {
				return fmt.Sprintf("%d x %s", t.A, t.B)
			})
			assert.Empty(t, f)
		})

		t.Run(fmt.Sprintf("Test %s string", suite.Name), func(t *testing.T) {
			f := suite.Impl(testCases, func(t TestCase) string {
				return fmt.Sprintf("%d x %s", t.A, t.B)
			})
			assert.Equal(t, "10 x hello", f[0])
			assert.Equal(t, "20 x bye", f[1])
		})

		t.Run(fmt.Sprintf("Test %s custom type", suite.Name), func(t *testing.T) {
			type custom struct {
				Foo int
				Bar string
			}
			customToString := func(c custom) string {
				return fmt.Sprintf("Foo: %d, Bar: %s", c.Foo, c.Bar)
			}

			f := suite.Impl(testCases, func(t TestCase) string {
				return customToString(custom{
					Foo: t.A * 2,
					Bar: t.B + t.B,
				})
			})
			assert.Equal(t, "Foo: 20, Bar: hellohello", f[0])
			assert.Equal(t, "Foo: 40, Bar: byebye", f[1])
		})
	}
}
