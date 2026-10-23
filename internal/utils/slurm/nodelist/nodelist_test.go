package nodelist_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"nebius.ai/slurm-operator/internal/utils/slurm/nodelist"
)

func TestNewNodeList(t *testing.T) {
	list := nodelist.NewNodeList([]string{"worker-3", "worker-1", "worker-2", "worker-cpu-0", "login", "worker-1"})
	assert.Equal(t, nodelist.NodeList("login,worker-[1-3],worker-cpu-0"), list)
	assert.Equal(t, nodelist.NodeList(""), nodelist.NewNodeList(nil))
}

func TestNodeNames(t *testing.T) {
	tests := []struct {
		list string
		want []string
	}{
		{list: "", want: nil},
		{list: "worker-0", want: []string{"worker-0"}},
		{list: "worker-[0-2]", want: []string{"worker-0", "worker-1", "worker-2"}},
		{list: "worker-[0-1,4,7-8]", want: []string{"worker-0", "worker-1", "worker-4", "worker-7", "worker-8"}},
		{list: "worker-[1-3],worker-cpu-0,login", want: []string{"worker-1", "worker-2", "worker-3", "worker-cpu-0", "login"}},
		{list: "gpu[099-101]", want: []string{"gpu099", "gpu100", "gpu101"}},
		{list: "gpu[001-002,004]", want: []string{"gpu001", "gpu002", "gpu004"}},
		{list: "rack[1-2]-gpu", want: []string{"rack1-gpu", "rack2-gpu"}},
		{list: "rack[1-2]-gpu[0-1]", want: []string{"rack1-gpu0", "rack1-gpu1", "rack2-gpu0", "rack2-gpu1"}},
		{list: "r[01-02]-n[7,3]-x", want: []string{"r01-n7-x", "r01-n3-x", "r02-n7-x", "r02-n3-x"}},
		{list: "worker-[8,0-1]", want: []string{"worker-8", "worker-0", "worker-1"}},
		{list: "worker-5,worker-[0-1]", want: []string{"worker-5", "worker-0", "worker-1"}},
		{list: " worker-[0-1] , worker-5 ", want: []string{"worker-0", "worker-1", "worker-5"}},
	}
	for _, tt := range tests {
		t.Run(tt.list, func(t *testing.T) {
			got, err := nodelist.NodeList(tt.list).NodeNames()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNodeNamesRejectsMalformedLists(t *testing.T) {
	for _, list := range []string{
		"worker-[0-1",
		"worker-0-1]",
		"worker-[[0-1]]",
		"worker-[]",
		"worker-[1-0]",
		"worker-[a-b]",
		"worker-[0-]",
		"worker-[-1]",
		"worker-0,,worker-1",
		"worker-0,",
		"a[1]b[2]c[3]",
	} {
		t.Run(list, func(t *testing.T) {
			_, err := nodelist.NodeList(list).NodeNames()
			assert.ErrorContains(t, err, fmt.Sprintf("parse node list %q", list))
		})
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		list string
		want string
	}{
		{list: "", want: ""},
		{list: "worker-[7-8,0-1],worker-3", want: "worker-[0-1,3,7-8]"},
		{list: "worker-1,worker-[0-2],worker-1", want: "worker-[0-2]"},
		{list: "rack10-[0-1],rack2-[0-1],login", want: "login,rack2-[0-1],rack10-[0-1]"},
		{list: "rack[1-2]-gpu-[0-1]", want: "rack1-gpu-[0-1],rack2-gpu-[0-1]"},
	}
	for _, tt := range tests {
		t.Run(tt.list, func(t *testing.T) {
			got, err := nodelist.NodeList(tt.list).Normalize()
			require.NoError(t, err)
			assert.Equal(t, nodelist.NodeList(tt.want), got)
		})
	}

	_, err := nodelist.NodeList("worker-[0-").Normalize()
	assert.ErrorContains(t, err, `normalize node list: parse node list "worker-[0-"`)
}

func TestRoundTrip(t *testing.T) {
	var names []string
	for i := 20; i >= 0; i-- {
		names = append(names, fmt.Sprintf("worker-%d", i))
	}
	names = append(names, "gpu001", "gpu002", "gpu004", "login")

	list := nodelist.NewNodeList(names)
	got, err := list.NodeNames()
	require.NoError(t, err)
	assert.ElementsMatch(t, names, got)
	assert.Equal(t, list, nodelist.NewNodeList(got))
}
