package e2e

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// NodeSetDef describes a single worker nodeset in the e2e profile.
type NodeSetDef struct {
	Name             string `json:"name"`
	Platform         string `json:"platform"`
	Preset           string `json:"preset"`
	Size             int    `json:"size"`
	InfinibandFabric string `json:"infiniband_fabric"`
	Preemptible      bool   `json:"preemptible"`
}

// Profile holds infrastructure-specific settings loaded from the PROFILE env var.
// JSON tags are required by sigs.k8s.io/yaml.
type Profile struct {
	NebiusProjectID string       `json:"nebius_project_id"`
	NebiusRegion    string       `json:"nebius_region"`
	NebiusTenantID  string       `json:"nebius_tenant_id"`
	NodeSets        []NodeSetDef `json:"nodesets"`
}

// Validate checks that the profile is well-formed.
func (p Profile) Validate() error {
	if len(p.NodeSets) == 0 {
		return fmt.Errorf("nodesets must not be empty")
	}

	seen := make(map[string]struct{}, len(p.NodeSets))
	for i, ns := range p.NodeSets {
		if ns.Name == "" {
			return fmt.Errorf("nodeset[%d]: name is required", i)
		}
		if ns.Platform == "" {
			return fmt.Errorf("nodeset[%d] %q: platform is required", i, ns.Name)
		}
		if ns.Preset == "" {
			return fmt.Errorf("nodeset[%d] %q: preset is required", i, ns.Name)
		}
		if ns.Size <= 0 {
			return fmt.Errorf("nodeset[%d] %q: size must be positive, got %d", i, ns.Name, ns.Size)
		}
		if _, ok := seen[ns.Name]; ok {
			return fmt.Errorf("nodeset[%d]: duplicate name %q", i, ns.Name)
		}
		seen[ns.Name] = struct{}{}
	}

	return nil
}

// LoadProfile reads the PROFILE env var (already-resolved YAML content) and returns a Profile.
func LoadProfile() (Profile, error) {
	raw := os.Getenv("E2E_PROFILE")
	if raw == "" {
		return Profile{}, fmt.Errorf("E2E_PROFILE env var is not set")
	}

	var p Profile
	if err := yaml.Unmarshal([]byte(raw), &p); err != nil {
		return Profile{}, fmt.Errorf("unmarshal profile YAML: %w", err)
	}

	if err := p.Validate(); err != nil {
		return Profile{}, fmt.Errorf("validate profile: %w", err)
	}

	return p, nil
}

//nolint:tagalign
type Config struct {
	SoperatorVersion   string `split_words:"true" required:"true"`                // SOPERATOR_VERSION
	SoperatorUnstable  bool   `split_words:"true" required:"true"`                // SOPERATOR_UNSTABLE
	PathToInstallation string `split_words:"true" required:"true"`                // PATH_TO_INSTALLATION
	O11yAccessToken    string `split_words:"true" required:"true"`                // O11Y_ACCESS_TOKEN
	O11ySecretName     string `split_words:"true" default:"o11y-writer-sa-token"` // O11Y_SECRET_NAME
	O11yNamespace      string `split_words:"true" default:"logs-system"`          // O11Y_NAMESPACE

	Profile      Profile `ignored:"true"`
	SSHPublicKey string  `ignored:"true"`
}
