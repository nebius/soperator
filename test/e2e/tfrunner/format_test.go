package tfrunner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToHCL(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name:     "nil value",
			input:    nil,
			expected: "null",
		},
		{
			name:     "bool true",
			input:    true,
			expected: "true",
		},
		{
			name:     "bool false",
			input:    false,
			expected: "false",
		},
		{
			name:     "int",
			input:    42,
			expected: "42",
		},
		{
			name:     "negative int",
			input:    -10,
			expected: "-10",
		},
		{
			name:     "float",
			input:    3.14,
			expected: "3.14",
		},
		{
			name:     "string",
			input:    "hello",
			expected: `"hello"`,
		},
		{
			name:     "string with quotes",
			input:    `say "hello"`,
			expected: `"say \"hello\""`,
		},
		{
			name:     "string with newline",
			input:    "line1\nline2",
			expected: `"line1\nline2"`,
		},
		{
			name:     "empty slice",
			input:    []string{},
			expected: "[]",
		},
		{
			name:     "string slice",
			input:    []string{"a", "b", "c"},
			expected: `["a", "b", "c"]`,
		},
		{
			name:     "int slice",
			input:    []int{1, 2, 3},
			expected: "[1, 2, 3]",
		},
		{
			name:     "empty map",
			input:    map[string]any{},
			expected: "{}",
		},
		{
			name:     "simple map",
			input:    map[string]any{"key": "value"},
			expected: `{key = "value"}`,
		},
		{
			name:     "map with multiple keys",
			input:    map[string]any{"a": 1, "b": 2},
			expected: `{a = 1, b = 2}`,
		},
		{
			name:     "nested map",
			input:    map[string]any{"outer": map[string]any{"inner": "value"}},
			expected: `{outer = {inner = "value"}}`,
		},
		{
			name:     "mixed types",
			input:    map[string]any{"enabled": true, "count": 5, "name": "test"},
			expected: `{count = 5, enabled = true, name = "test"}`,
		},
		{
			name:     "slice of maps",
			input:    []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}},
			expected: `[{name = "a"}, {name = "b"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toHCL(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatTerraformVarsAsArgs(t *testing.T) {
	tests := []struct {
		name     string
		vars     map[string]any
		expected []string
	}{
		{
			name:     "empty vars",
			vars:     map[string]any{},
			expected: nil,
		},
		{
			name:     "single string var",
			vars:     map[string]any{"name": "test"},
			expected: []string{"-var", `name="test"`},
		},
		{
			name:     "multiple vars sorted",
			vars:     map[string]any{"z_var": 1, "a_var": 2},
			expected: []string{"-var", "a_var=2", "-var", "z_var=1"},
		},
		{
			name: "complex var",
			vars: map[string]any{
				"config": map[string]any{
					"enabled": true,
					"count":   3,
				},
			},
			expected: []string{"-var", "config={count = 3, enabled = true}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatTerraformVarsAsArgs(tt.vars)
			assert.Equal(t, tt.expected, result)
		})
	}
}
