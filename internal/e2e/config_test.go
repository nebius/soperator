package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func validProfile() Profile {
	return Profile{
		NebiusProjectID: "project-123",
		NebiusRegion:    "eu-north1",
		NebiusTenantID:  "tenant-456",
		NodeSets: []NodeSetDef{
			{
				Name:             "worker-gpu",
				Platform:         "gpu-h100-sxm",
				Preset:           "8gpu-128vcpu-1600gb",
				Size:             2,
				InfinibandFabric: "cuda",
			},
		},
	}
}

func TestValidate_Valid(t *testing.T) {
	assert.NoError(t, validProfile().Validate())
}

func TestValidate_MultipleNodeSets(t *testing.T) {
	p := validProfile()
	p.NodeSets = append(p.NodeSets, NodeSetDef{
		Name:     "worker-cpu",
		Platform: "cpu",
		Preset:   "16vcpu-64gb",
		Size:     3,
	})
	assert.NoError(t, p.Validate())
}

func TestValidate_EmptyNodeSets(t *testing.T) {
	p := validProfile()
	p.NodeSets = nil
	assert.ErrorContains(t, p.Validate(), "nodesets must not be empty")
}

func TestValidate_DuplicateNames(t *testing.T) {
	p := validProfile()
	p.NodeSets = append(p.NodeSets, p.NodeSets[0])
	assert.ErrorContains(t, p.Validate(), "duplicate name")
}

func TestValidate_MissingName(t *testing.T) {
	p := validProfile()
	p.NodeSets[0].Name = ""
	assert.ErrorContains(t, p.Validate(), "name is required")
}

func TestValidate_MissingPlatform(t *testing.T) {
	p := validProfile()
	p.NodeSets[0].Platform = ""
	assert.ErrorContains(t, p.Validate(), "platform is required")
}

func TestValidate_MissingPreset(t *testing.T) {
	p := validProfile()
	p.NodeSets[0].Preset = ""
	assert.ErrorContains(t, p.Validate(), "preset is required")
}

func TestValidate_ZeroSize(t *testing.T) {
	p := validProfile()
	p.NodeSets[0].Size = 0
	assert.ErrorContains(t, p.Validate(), "size must be positive")
}

func TestValidate_NegativeSize(t *testing.T) {
	p := validProfile()
	p.NodeSets[0].Size = -1
	assert.ErrorContains(t, p.Validate(), "size must be positive")
}
