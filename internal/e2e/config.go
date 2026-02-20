package e2e

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// Profile holds infrastructure-specific settings loaded from the PROFILE env var.
// JSON tags are required by sigs.k8s.io/yaml.
type Profile struct {
	NebiusProjectID  string `json:"nebius_project_id"`
	NebiusRegion     string `json:"nebius_region"`
	NebiusTenantID   string `json:"nebius_tenant_id"`
	InfinibandFabric string `json:"infiniband_fabric"`
	WorkerPlatform   string `json:"worker_platform"`
	WorkerPreset     string `json:"worker_preset"`
	PreemptibleNodes bool   `json:"preemptible_nodes"`
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
