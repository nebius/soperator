package steps

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Note: sampleTopologyOutput uses literal comma-separated node lists instead
// of Slurm's bracket-compressed form so tests can call parseTopology with a
// nil expander. Bracket expansion is tested separately in
// TestParseTopologyExpandsHostlistInSwitches.
const sampleTopologyOutput = `
SwitchName=185ccb815910fcd0a3d1693651b78d1d Level=0 LinkSpeed=1 Nodes=worker-1
SwitchName=4d63b28c08a3871d951ee886e3e35f14 Level=0 LinkSpeed=1 Nodes=worker-0
SwitchName=c77d58ca27418e0713112f6974e3a7e3 Level=1 LinkSpeed=1 Nodes=worker-0 Switches=4d63b28c08a3871d951ee886e3e35f14
SwitchName=e4408eeb4933a2a8c6141a1066a974d8 Level=1 LinkSpeed=1 Nodes=worker-1 Switches=185ccb815910fcd0a3d1693651b78d1d
SwitchName=root Level=2 LinkSpeed=1 Nodes=worker-0,worker-1,worker-dynamic-0,worker-payg-0 Switches=c77d58ca27418e0713112f6974e3a7e3,e4408eeb4933a2a8c6141a1066a974d8,unknown
SwitchName=unknown Level=0 LinkSpeed=1 Nodes=worker-dynamic-0,worker-payg-0
`

func assertEdgeSet(t *testing.T, label string, got map[string]bool, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: size=%d want %d (got=%v want=%v)", label, len(got), len(want), keys(got), want)
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("%s: missing %q", label, w)
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func collectRoots(g *topologyGraph) map[string]bool {
	out := map[string]bool{}
	for name, n := range g.nodes {
		if n.isRoot {
			out[name] = true
		}
	}
	return out
}

func collectWorkers(g *topologyGraph) map[string]bool {
	out := map[string]bool{}
	for name, n := range g.nodes {
		if n.isWorker {
			out[name] = true
		}
	}
	return out
}

func targetsOf(g *topologyGraph, name string) map[string]bool {
	n := g.nodes[name]
	if n == nil {
		return nil
	}
	return n.targets
}

func boolSet(m map[string]string) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func TestParseTopologyGraphFromSampleOutput(t *testing.T) {
	g, err := parseTopology(sampleTopologyOutput, nil, nil)
	if err != nil {
		t.Fatalf("parseTopology: %v", err)
	}

	assertEdgeSet(t, "roots", collectRoots(g), []string{"root"})

	// root has both child switches and Nodes=worker-0,worker-1,... attached
	// directly, so its target set is the union of both.
	assertEdgeSet(t, "nodes[root].targets", targetsOf(g, "root"), []string{
		"c77d58ca27418e0713112f6974e3a7e3",
		"e4408eeb4933a2a8c6141a1066a974d8",
		"unknown",
		"worker-0",
		"worker-1",
		"worker-dynamic-0",
		"worker-payg-0",
	})
	assertEdgeSet(t, "nodes[c77d58...].targets", targetsOf(g, "c77d58ca27418e0713112f6974e3a7e3"),
		[]string{"4d63b28c08a3871d951ee886e3e35f14", "worker-0"})
	assertEdgeSet(t, "nodes[e4408e...].targets", targetsOf(g, "e4408eeb4933a2a8c6141a1066a974d8"),
		[]string{"185ccb815910fcd0a3d1693651b78d1d", "worker-1"})
	assertEdgeSet(t, "nodes[unknown].targets", targetsOf(g, "unknown"),
		[]string{"worker-dynamic-0", "worker-payg-0"})
	assertEdgeSet(t, "nodes[4d63b28c...].targets", targetsOf(g, "4d63b28c08a3871d951ee886e3e35f14"),
		[]string{"worker-0"})
	assertEdgeSet(t, "nodes[185ccb...].targets", targetsOf(g, "185ccb815910fcd0a3d1693651b78d1d"),
		[]string{"worker-1"})

	assertEdgeSet(t, "workers", collectWorkers(g),
		[]string{"worker-0", "worker-1", "worker-dynamic-0", "worker-payg-0"})

	wantLevels := map[string]int{
		"185ccb815910fcd0a3d1693651b78d1d": 0,
		"4d63b28c08a3871d951ee886e3e35f14": 0,
		"c77d58ca27418e0713112f6974e3a7e3": 1,
		"e4408eeb4933a2a8c6141a1066a974d8": 1,
		"root":                             2,
		"unknown":                          0,
	}
	for sw, want := range wantLevels {
		n := g.nodes[sw]
		if n == nil {
			t.Errorf("levels[%s]: node missing", sw)
			continue
		}
		if n.level != want {
			t.Errorf("levels[%s]=%d, want %d", sw, n.level, want)
		}
	}
}

func TestValidateReportedPathAcceptsObservedOutput(t *testing.T) {
	g, err := parseTopology(sampleTopologyOutput, nil, nil)
	if err != nil {
		t.Fatalf("parseTopology: %v", err)
	}

	accepted := map[string]string{
		"worker-0":         "root.c77d58ca27418e0713112f6974e3a7e3.4d63b28c08a3871d951ee886e3e35f14.worker-0",
		"worker-1":         "root.e4408eeb4933a2a8c6141a1066a974d8.185ccb815910fcd0a3d1693651b78d1d.worker-1",
		"worker-dynamic-0": "root..unknown.worker-dynamic-0",
		"worker-payg-0":    "root..unknown.worker-payg-0",
	}

	workers := keys(boolSet(accepted))
	for _, worker := range workers {
		if err := validateReportedPath(g, accepted[worker], worker); err != nil {
			t.Errorf("validateReportedPath(%q, %q): %v", accepted[worker], worker, err)
		}
	}
}

func TestValidateReportedPathRejectsBadPaths(t *testing.T) {
	g, err := parseTopology(sampleTopologyOutput, nil, nil)
	if err != nil {
		t.Fatalf("parseTopology: %v", err)
	}

	cases := []struct {
		name     string
		path     string
		worker   string
		wantSubs string // substring we expect in the error
	}{
		{
			name:     "tail is a different worker than claimed",
			path:     "root.c77d58ca27418e0713112f6974e3a7e3.4d63b28c08a3871d951ee886e3e35f14.worker-0",
			worker:   "worker-1",
			wantSubs: "does not end with worker",
		},
		{
			name:     "non-root prefix",
			path:     "badroot.c77d58ca27418e0713112f6974e3a7e3.4d63b28c08a3871d951ee886e3e35f14.worker-0",
			worker:   "worker-0",
			wantSubs: "does not start at a root",
		},
		{
			name:     "crossed branches (no such edge)",
			path:     "root.e4408eeb4933a2a8c6141a1066a974d8.4d63b28c08a3871d951ee886e3e35f14.worker-0",
			worker:   "worker-0",
			wantSubs: "no edge",
		},
		{
			name:     "worker not attached to claimed leaf",
			path:     "root.c77d58ca27418e0713112f6974e3a7e3.4d63b28c08a3871d951ee886e3e35f14.worker-dynamic-0",
			worker:   "worker-dynamic-0",
			wantSubs: "is not attached",
		},
		{
			name:     "single segment",
			path:     "worker-0",
			worker:   "worker-0",
			wantSubs: "malformed path",
		},
		{
			name:     "path truncated to root (worker slips into switch slot)",
			path:     "root.worker-0",
			worker:   "worker-0",
			wantSubs: "has 2 segments, expected 4",
		},
		{
			name:     "path truncated to mid-level switch",
			path:     "root.c77d58ca27418e0713112f6974e3a7e3.worker-0",
			worker:   "worker-0",
			wantSubs: "has 3 segments, expected 4",
		},
		{
			name:     "switch appears at wrong level",
			path:     "root.4d63b28c08a3871d951ee886e3e35f14.c77d58ca27418e0713112f6974e3a7e3.worker-0",
			worker:   "worker-0",
			wantSubs: "expected 1",
		},
		{
			name:     "path skips leaf level entirely (all intermediate slots empty)",
			path:     "root...worker-0",
			worker:   "worker-0",
			wantSubs: "leaf slot at position 2 is empty",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateReportedPath(g, tc.path, tc.worker)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubs) {
				t.Errorf("err=%q, want substring %q", err, tc.wantSubs)
			}
		})
	}
}

func TestParseTopologyFlatNodesALL(t *testing.T) {
	raw := "SwitchName=root Level=0 Nodes=ALL"
	g, err := parseTopology(raw, []string{"worker-0", "worker-1"}, nil)
	if err != nil {
		t.Fatalf("parseTopology: %v", err)
	}
	assertEdgeSet(t, "roots", collectRoots(g), []string{"root"})
	// Flat topology: the only switch (root) has workers as direct edge
	// targets, and no switch has any outgoing edge to another switch.
	assertEdgeSet(t, "nodes[root].targets", targetsOf(g, "root"), []string{"worker-0", "worker-1"})
	for name, n := range g.nodes {
		if n.isWorker || name == "root" {
			continue
		}
		t.Errorf("unexpected extra switch %q with targets %v", name, n.targets)
	}

	for _, worker := range []string{"worker-0", "worker-1"} {
		path := "root." + worker
		if err := validateReportedPath(g, path, worker); err != nil {
			t.Errorf("validateReportedPath(%q, %q): %v", path, worker, err)
		}
	}
}

func TestParseTopologyRejectsDottedSwitchNames(t *testing.T) {
	cases := []string{
		"SwitchName=rack-1.leaf Level=0 Nodes=worker-0",
		"SwitchName=root Level=1 Switches=rack-1.leaf Nodes=worker-0",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			_, err := parseTopology(raw, nil, nil)
			if err == nil {
				t.Fatal("expected error for dotted switch name, got nil")
			}
			if !strings.Contains(err.Error(), "'.'") {
				t.Errorf("err=%q, want substring %q", err, "'.'")
			}
		})
	}
}

func TestParseTopologyExpandsHostlistInSwitches(t *testing.T) {
	raw := `
SwitchName=leaf01 Level=0 Nodes=worker-0
SwitchName=leaf02 Level=0 Nodes=worker-1
SwitchName=root Level=1 Switches=leaf[01-02] Nodes=worker-[0-1]
`
	expand := func(value string) ([]string, error) {
		switch value {
		case "leaf[01-02]":
			return []string{"leaf01", "leaf02"}, nil
		case "worker-[0-1]":
			return []string{"worker-0", "worker-1"}, nil
		}
		return nil, fmt.Errorf("unexpected expand %q", value)
	}
	g, err := parseTopology(raw, nil, expand)
	if err != nil {
		t.Fatalf("parseTopology: %v", err)
	}
	assertEdgeSet(t, "roots", collectRoots(g), []string{"root"})
	assertEdgeSet(t, "nodes[root].targets", targetsOf(g, "root"),
		[]string{"leaf01", "leaf02", "worker-0", "worker-1"})
	assertEdgeSet(t, "nodes[leaf01].targets", targetsOf(g, "leaf01"), []string{"worker-0"})
	assertEdgeSet(t, "nodes[leaf02].targets", targetsOf(g, "leaf02"), []string{"worker-1"})
}

func TestParseTopologyForestHasMultipleRoots(t *testing.T) {
	raw := `
SwitchName=treeA Level=0 Nodes=worker-a
SwitchName=treeB Level=0 Nodes=worker-b
`
	g, err := parseTopology(raw, nil, nil)
	if err != nil {
		t.Fatalf("parseTopology: %v", err)
	}
	assertEdgeSet(t, "roots", collectRoots(g), []string{"treeA", "treeB"})
	assertEdgeSet(t, "nodes[treeA].targets", targetsOf(g, "treeA"), []string{"worker-a"})
	assertEdgeSet(t, "nodes[treeB].targets", targetsOf(g, "treeB"), []string{"worker-b"})

	// Each worker is accepted on its own root.
	if err := validateReportedPath(g, "treeA.worker-a", "worker-a"); err != nil {
		t.Errorf("treeA.worker-a: %v", err)
	}
	if err := validateReportedPath(g, "treeB.worker-b", "worker-b"); err != nil {
		t.Errorf("treeB.worker-b: %v", err)
	}

	// Cross-root: worker-a claimed under treeB should fail (not attached).
	if err := validateReportedPath(g, "treeB.worker-a", "worker-a"); err == nil {
		t.Errorf("cross-root should fail, got nil")
	}
}
