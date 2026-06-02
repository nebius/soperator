package pattern_test

import (
	"fmt"
	"testing"

	slurmpattern "nebius.ai/slurm-operator/internal/utils/slurm/pattern"
)

func TestMerge(t *testing.T) {
	entities := []string{
		"worker-cpu-4",
		"worker-cpu-1",
		"worker-cpu-0",
		"workerkek1",
	}

	for i := 15; i >= 0; i-- {
		entities = append(entities, fmt.Sprintf("worker-%d", i))
	}
	for i := 7; i >= 0; i-- {
		entities = append(entities, fmt.Sprintf("worker-rack1-%d", i))
		entities = append(entities, fmt.Sprintf("worker-rack0-%d", i))
	}

	got := slurmpattern.Merge(entities)
	want := "worker-[0-15],worker-cpu-[0-1,4],worker-rack0-[0-7],worker-rack1-[0-7],workerkek1"
	if got != want {
		t.Fatalf("Merge(%v) = %q, want %q", entities, got, want)
	}
}

func TestMergePrefixed(t *testing.T) {
	tests := []struct {
		name     string
		entities []string
		prefix   string
		want     string
	}{
		{
			name: "merges sorted numeric ranges",
			entities: []string{
				"dev12",
				"dev2",
				"dev1",
				"dev8",
				"dev0",
				"dev7",
				"dev4",
				"dev3",
				"dev5",
			},
			prefix: "dev",
			want:   "dev[0-5,7-8,12]",
		},
		{
			name:     "deduplicates entities",
			entities: []string{"dev2", "dev1", "dev2", "dev3"},
			prefix:   "dev",
			want:     "dev[1-3]",
		},
		{
			name:     "keeps single entity unbracketed",
			entities: []string{"dev12"},
			prefix:   "dev",
			want:     "dev12",
		},
		{
			name:     "supports prefixes with separators",
			entities: []string{"worker-3", "worker-1", "worker-2"},
			prefix:   "worker-",
			want:     "worker-[1-3]",
		},
		{
			name:     "preserves zero padding",
			entities: []string{"gpu004", "gpu001", "gpu002"},
			prefix:   "gpu",
			want:     "gpu[001-002,004]",
		},
		{
			name:     "preserves zero padding across digit boundary",
			entities: []string{"gpu100", "gpu099", "gpu101"},
			prefix:   "gpu",
			want:     "gpu[099-101]",
		},
		{
			name:     "keeps mixed suffix widths separate",
			entities: []string{"dev1", "dev002", "dev003"},
			prefix:   "dev",
			want:     "dev1,dev[002-003]",
		},
		{
			name:     "keeps non matching entities",
			entities: []string{"dev0", "other2", "devx", "dev2"},
			prefix:   "dev",
			want:     "dev[0,2],devx,other2",
		},
		{
			name:     "empty input",
			entities: nil,
			prefix:   "dev",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slurmpattern.MergePrefixed(tt.entities, tt.prefix)
			if got != tt.want {
				t.Fatalf("MergePrefixed(%v, %q) = %q, want %q", tt.entities, tt.prefix, got, tt.want)
			}
		})
	}
}
