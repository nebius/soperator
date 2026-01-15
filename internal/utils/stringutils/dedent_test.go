package stringutils_test

import (
	"testing"

	. "nebius.ai/slurm-operator/internal/utils/stringutils"
)

func TestDedent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty_string",
			input: "",
			want:  "",
		},
		{
			name:  "only_whitespace_lines",
			input: "   \n\t\n  \t  ",
			want:  "",
		},
		{
			name:  "single_line_no_indent",
			input: "hello",
			want:  "hello",
		},
		{
			name: "multiline_uniform_indent_with_outer_empty_lines",
			input: `
    foo
    bar
`,
			want: "foo\nbar",
		},
		{
			name:  "multiline_mixed_indent_min_indent_applied",
			input: "  foo\n    bar\n  baz",
			want:  "foo\n  bar\nbaz",
		},
		{
			name: "internal_blank_lines_preserved",
			input: `
    foo

    bar
`,
			want: "foo\n\nbar",
		},
		{
			name:  "tabs_as_indentation",
			input: "\n\t\tfoo\n\t\tbar\n",
			want:  "foo\nbar",
		},
		{
			name:  "blank_lines_not_affect_indent_detection",
			input: "\n    foo\n\n        bar\n",
			want:  "foo\n\n    bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Dedent(tt.input)
			if got != tt.want {
				t.Errorf("Dedent() = %q, want %q", got, tt.want)
			}
		})
	}
}
