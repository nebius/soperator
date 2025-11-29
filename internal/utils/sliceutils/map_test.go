package sliceutils_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"nebius.ai/slurm-operator/internal/utils/sliceutils"
)

func TestMap(t *testing.T) {
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

func TestMapSlice(t *testing.T) {
	t.Run("Test MapSlice empty", func(t *testing.T) {
		f := sliceutils.MapSlice(emptyTestCases, func(t TestCase) string {
			return fmt.Sprintf("%d x %s", t.A, t.B)
		})
		assert.Empty(t, f)
	})

	t.Run("Test MapSlice string", func(t *testing.T) {
		f := sliceutils.MapSlice(testCases, func(t TestCase) string {
			return fmt.Sprintf("%d x %s", t.A, t.B)
		})
		assert.Equal(t, "10 x hello", f[0])
		assert.Equal(t, "20 x bye", f[1])
	})

	t.Run("Test MapSlice custom type", func(t *testing.T) {
		type custom struct {
			Foo int
			Bar string
		}
		f := sliceutils.MapSlice(testCases, func(t TestCase) custom {
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

func TestMapSliceSeq(t *testing.T) {
	t.Run("Test MapSliceSeq empty", func(t *testing.T) {
		f := sliceutils.MapSliceSeq(emptyTestCases, func(t TestCase) string {
			return fmt.Sprintf("%d x %s", t.A, t.B)
		})
		assert.Empty(t, sliceutils.Collect(f))
	})

	t.Run("Test MapSliceSeq string", func(t *testing.T) {
		f := sliceutils.MapSliceSeq(testCases, func(t TestCase) string {
			return fmt.Sprintf("%d x %s", t.A, t.B)
		})
		assert.Equal(t, "10 x hello", sliceutils.Collect(f)[0])
		assert.Equal(t, "20 x bye", sliceutils.Collect(f)[1])
	})

	t.Run("Test MapSliceSeq custom type", func(t *testing.T) {
		type custom struct {
			Foo int
			Bar string
		}
		f := sliceutils.MapSliceSeq(testCases, func(t TestCase) custom {
			return custom{
				Foo: t.A * 2,
				Bar: t.B + t.B,
			}
		})
		assert.Equal(t, 20, sliceutils.Collect(f)[0].Foo)
		assert.Equal(t, 40, sliceutils.Collect(f)[1].Foo)
		assert.Equal(t, "hellohello", sliceutils.Collect(f)[0].Bar)
		assert.Equal(t, "byebye", sliceutils.Collect(f)[1].Bar)
	})
}

func TestMapSeqSlice(t *testing.T) {
	t.Run("Test MapSeqSlice empty", func(t *testing.T) {
		f := sliceutils.MapSeqSlice(sliceutils.SliceSeq(emptyTestCases), func(t TestCase) string {
			return fmt.Sprintf("%d x %s", t.A, t.B)
		})
		assert.Empty(t, f)
	})

	t.Run("Test MapSeqSlice string", func(t *testing.T) {
		f := sliceutils.MapSeqSlice(sliceutils.SliceSeq(testCases), func(t TestCase) string {
			return fmt.Sprintf("%d x %s", t.A, t.B)
		})
		assert.Equal(t, "10 x hello", f[0])
		assert.Equal(t, "20 x bye", f[1])
	})

	t.Run("Test MapSeqSlice custom type", func(t *testing.T) {
		type custom struct {
			Foo int
			Bar string
		}
		f := sliceutils.MapSeqSlice(sliceutils.SliceSeq(testCases), func(t TestCase) custom {
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

func TestMapSeq(t *testing.T) {
	t.Run("Test MapSeq empty", func(t *testing.T) {
		f := sliceutils.MapSeq(sliceutils.SliceSeq(emptyTestCases), func(t TestCase) string {
			return fmt.Sprintf("%d x %s", t.A, t.B)
		})
		assert.Empty(t, sliceutils.Collect(f))
	})

	t.Run("Test MapSeq string", func(t *testing.T) {
		f := sliceutils.MapSeq(sliceutils.SliceSeq(testCases), func(t TestCase) string {
			return fmt.Sprintf("%d x %s", t.A, t.B)
		})
		assert.Equal(t, "10 x hello", sliceutils.Collect(f)[0])
		assert.Equal(t, "20 x bye", sliceutils.Collect(f)[1])
	})

	t.Run("Test MapSeq custom type", func(t *testing.T) {
		type custom struct {
			Foo int
			Bar string
		}
		f := sliceutils.MapSeq(sliceutils.SliceSeq(testCases), func(t TestCase) custom {
			return custom{
				Foo: t.A * 2,
				Bar: t.B + t.B,
			}
		})
		assert.Equal(t, 20, sliceutils.Collect(f)[0].Foo)
		assert.Equal(t, 40, sliceutils.Collect(f)[1].Foo)
		assert.Equal(t, "hellohello", sliceutils.Collect(f)[0].Bar)
		assert.Equal(t, "byebye", sliceutils.Collect(f)[1].Bar)
	})
}
