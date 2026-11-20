package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/strings/slices"

	slurmassignment "nebius.ai/slurm-operator/internal/utils/slurm/assignment"
)

func TestFilterNodeStatic(t *testing.T) {
	tests := []struct {
		name            string
		static          string
		dropSuspendTime bool
		expected        string
	}{
		{
			name:     "empty",
			static:   "",
			expected: "",
		},
		{
			name:     "nothing to remove keeps the line unchanged",
			static:   "  CPUs=4\tBoards=1  SuspendTime=600 ",
			expected: "  CPUs=4\tBoards=1  SuspendTime=600 ",
		},
		{
			name:     "exact Feature and State are removed",
			static:   "Feature=a CPUs=4 State=DOWN Boards=1 Feature=b",
			expected: "CPUs=4 Boards=1",
		},
		{
			name:     "other Feature spellings are kept",
			static:   "feature=existing-license Feature = existing-license Features=x CPUs=4\tFeature=tabbed",
			expected: "feature=existing-license Feature = existing-license Features=x CPUs=4\tFeature=tabbed",
		},
		{
			name:     "Feature after text that is not an assignment is removed",
			static:   "bogus Feature=a CPUs=4",
			expected: "bogus CPUs=4",
		},
		{
			name:     "word with Feature and a tab-separated State is removed as a whole",
			static:   "Feature=a\tState=DOWN CPUs=4",
			expected: "CPUs=4",
		},
		{
			name:     "word with Feature and a newline-separated State is removed as a whole",
			static:   "CPUs=4 Feature=a\nState=DOWN",
			expected: "CPUs=4",
		},
		{
			name:     "empty Feature and State are removed",
			static:   "Feature= CPUs=4 State=",
			expected: "CPUs=4",
		},
		{
			name:            "SuspendTime inside a removed Feature word is removed with it",
			static:          "Feature=a\tSuspendTime=600 CPUs=4",
			dropSuspendTime: true,
			expected:        "CPUs=4",
		},
		{
			name:            "SuspendTime after an empty Feature is removed",
			static:          "Feature= SuspendTime=600 CPUs=4",
			dropSuspendTime: true,
			expected:        "CPUs=4",
		},
		{
			name:            "SuspendTime after an empty State is removed",
			static:          "CPUs=4 State= SuspendTime = 600",
			dropSuspendTime: true,
			expected:        "CPUs=4",
		},
		{
			name:            "SuspendTime joined by a removal is removed too",
			static:          "SuspendTime suspendtime= SuspendTime = 5 CPUs=4",
			dropSuspendTime: true,
			expected:        "CPUs=4",
		},
		{
			name:            "removals at the start, middle and end",
			static:          "SuspendTime=1 State=DOWN CPUs=4 Feature=a SuspendTime = 2 Boards=1 State=",
			dropSuspendTime: true,
			expected:        "CPUs=4 Boards=1",
		},
		{
			name:            "SuspendTime is kept without dropSuspendTime",
			static:          "CPUs=4 SuspendTime=600",
			dropSuspendTime: false,
			expected:        "CPUs=4 SuspendTime=600",
		},
		{
			name:            "SuspendTime in every Slurm spelling is removed",
			static:          "SuspendTime=1 CPUs=4 suspendtime=2 SuspendTime = 3 SuspendTime= 4 SUSPENDTIME =5 SuspendTime+=6 Boards=1",
			dropSuspendTime: true,
			expected:        "CPUs=4 Boards=1",
		},
		{
			name:            "quoted SuspendTime is removed",
			static:          `CPUs=4 SuspendTime="600" Boards=1`,
			dropSuspendTime: true,
			expected:        "CPUs=4 Boards=1",
		},
		{
			name:            "SuspendTime inside another quoted value is kept",
			static:          `Reason="keep SuspendTime=600 here" SuspendTime=600 CPUs=4`,
			dropSuspendTime: true,
			expected:        `Reason="keep SuspendTime=600 here" CPUs=4`,
		},
		{
			name:            "SuspendTimeout is kept",
			static:          "SuspendTimeout=90 SuspendTime=600",
			dropSuspendTime: true,
			expected:        "SuspendTimeout=90",
		},
		{
			name:            "SuspendTime removal keeps surrounding whitespace and tabbed Feature",
			static:          "CPUs=4\tSuspendTime=600\tFeature=existing-license",
			dropSuspendTime: true,
			expected:        "CPUs=4\tFeature=existing-license",
		},
		{
			name:            "SuspendTime after text that is not an assignment is removed",
			static:          "CPUs=4 SuspendTime=600 bogus SuspendTime=700",
			dropSuspendTime: true,
			expected:        "CPUs=4 bogus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, filterNodeStatic(tt.static, tt.dropSuspendTime))
		})
	}
}

// TestFilterNodeStaticMatchesLegacyFilter checks that, without dropSuspendTime, filterNodeStatic gives the same
// result as the split-and-prefix filter it replaced. The words include other whitespace, empty values and quotes,
// which the legacy filter does not treat as separators.
func TestFilterNodeStaticMatchesLegacyFilter(t *testing.T) {
	legacy := func(static string) string {
		return strings.Join(
			slices.Filter(nil, strings.Split(static, " "), func(s string) bool {
				return !strings.HasPrefix(s, "Feature=") && !strings.HasPrefix(s, "State=")
			}),
			" ",
		)
	}
	words := []string{
		"CPUs=4", "Feature=a,b", "State=DOWN", "feature=x", "Features=y", "Feature = z", "Feature=", "State=",
		"Feature=a\tState=DOWN", "CPUs=4\tState=DOWN", "State=DOWN\nCPUs=4", "\t", `Reason="a`, `State=b"`,
		"Gres=gpu:nvidia-h100:8", "SuspendTime=600", "bogus", "",
	}
	separators := []string{" ", "  "}

	lines := []string{""}
	for range 3 {
		var next []string
		for _, line := range lines {
			for _, word := range words {
				if line == "" {
					next = append(next, word)
					continue
				}
				for _, separator := range separators {
					next = append(next, line+separator+word)
				}
			}
		}
		lines = append(lines, next...)
	}

	for _, line := range lines {
		assert.Equal(t, legacy(line), filterNodeStatic(line, false), "static %q", line)
	}
}

// TestFilterNodeStaticLeavesNoSuspendTime checks that, with dropSuspendTime, Slurm finds no SuspendTime
// assignment in the result, including when empty values or other whitespace change how the line is split.
func TestFilterNodeStaticLeavesNoSuspendTime(t *testing.T) {
	words := []string{
		"Feature=", "State=", "Feature=a", "SuspendTime=600", "SuspendTime = 5", "suspendtime=", "SuspendTime",
		"=", "600", "CPUs=4", "Weight=", "Feature=a\tSuspendTime=1", "CPUs=4\tSuspendTime=2", `Reason="a`, "bogus",
	}

	lines := []string{""}
	for range 3 {
		var next []string
		for _, line := range lines {
			for _, word := range words {
				if line == "" {
					next = append(next, word)
				} else {
					next = append(next, line+" "+word)
				}
			}
		}
		lines = append(lines, next...)
	}

	for _, line := range lines {
		filtered := filterNodeStatic(line, true)
		for a := range slurmassignment.All(filtered) {
			assert.NotEqualf(t, "suspendtime", strings.ToLower(a.Key), "static %q rendered as %q", line, filtered)
		}
	}
}
