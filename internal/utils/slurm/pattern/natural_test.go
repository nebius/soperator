package pattern_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	slurmpattern "nebius.ai/slurm-operator/internal/utils/slurm/pattern"
)

func TestNaturalCompare(t *testing.T) {
	names := []string{"rack10-", "rack2-", "rack1-", "login", "rack02-", "rack2a", "worker-", "rack2-b"}
	slices.SortFunc(names, slurmpattern.NaturalCompare)
	assert.Equal(t, []string{"login", "rack1-", "rack02-", "rack2-", "rack2-b", "rack2a", "rack10-", "worker-"}, names)
	assert.Equal(t, 0, slurmpattern.NaturalCompare("a1", "a1"))
}

func TestMergeOrdersEntriesNaturally(t *testing.T) {
	got := slurmpattern.Merge([]string{"rack10-0", "rack2-1", "rack2-0", "login", "rack1-5"})
	assert.Equal(t, "login,rack1-5,rack2-[0-1],rack10-0", got)
}
