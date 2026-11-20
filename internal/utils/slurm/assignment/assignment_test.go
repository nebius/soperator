package assignment

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAll(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected []Assignment
	}{
		{
			name: "empty line",
			line: "",
		},
		{
			name: "plain assignments",
			line: "CPUs=4 Gres=gpu:8",
			expected: []Assignment{
				{Key: "CPUs", Value: "4", Start: 0, End: 6},
				{Key: "Gres", Value: "gpu:8", Start: 7, End: 17},
			},
		},
		{
			name: "whitespace around equals sign",
			line: "A = 1 B= 2 C =3",
			expected: []Assignment{
				{Key: "A", Value: "1", Start: 0, End: 5},
				{Key: "B", Value: "2", Start: 6, End: 10},
				{Key: "C", Value: "3", Start: 11, End: 15},
			},
		},
		{
			name: "tabs and repeated whitespace",
			line: "  A=1\t\tB=2  ",
			expected: []Assignment{
				{Key: "A", Value: "1", Start: 2, End: 5},
				{Key: "B", Value: "2", Start: 7, End: 10},
			},
		},
		{
			name: "operator",
			line: "SuspendTime+=600",
			expected: []Assignment{
				{Key: "SuspendTime", Value: "600", Start: 0, End: 16},
			},
		},
		{
			name: "quoted value with whitespace and assignment text",
			line: `Reason="keep SuspendTime=600 here" A=1`,
			expected: []Assignment{
				{Key: "Reason", Value: "keep SuspendTime=600 here", Start: 0, End: 34},
				{Key: "A", Value: "1", Start: 35, End: 38},
			},
		},
		{
			name: "empty quoted value",
			line: `A="" B=1`,
			expected: []Assignment{
				{Key: "A", Value: "", Start: 0, End: 4},
				{Key: "B", Value: "1", Start: 5, End: 8},
			},
		},
		{
			name: "text that is not an assignment is skipped",
			line: "A=1 bogus B=2 trailing",
			expected: []Assignment{
				{Key: "A", Value: "1", Start: 0, End: 3},
				{Key: "B", Value: "2", Start: 10, End: 13},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, slices.Collect(All(tt.line)))
		})
	}
}

func TestAllStopsWhenYieldReturnsFalse(t *testing.T) {
	var keys []string
	for a := range All("A=1 B=2 C=3") {
		keys = append(keys, a.Key)
		if a.Key == "B" {
			break
		}
	}
	assert.Equal(t, []string{"A", "B"}, keys)
}
