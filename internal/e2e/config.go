package e2e

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// NodeSetDef describes a single worker nodeset in the e2e profile.
type NodeSetDef struct {
	Name               string                 `json:"name"`
	Platform           string                 `json:"platform"`
	Preset             string                 `json:"preset"`
	Size               int                    `json:"size"`
	InfinibandFabric   string                 `json:"infiniband_fabric"`
	Preemptible        bool                   `json:"preemptible"`
	TerraformOverrides map[string]interface{} `json:"terraform_overrides,omitempty"`
}

type CapacityStrategy string

const (
	CapacityStrategyWarn   CapacityStrategy = "warn"
	CapacityStrategyCancel CapacityStrategy = "cancel"
)

// LabelPrefix marks a profile request as a label rather than a profile name:
// a request `@x` picks among the profiles carrying label x, while `MAN_H100` picks that one profile.
// Which labels exist is entirely up to the config; none is special to this code.
const LabelPrefix = "@"

var labelRe = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

// NormalizeLabel maps the ways a label can be spelled onto its canonical form.
// The leading "@" that marks a label in the profile input is accepted here too, so
// `@x` and `x` mean the same thing wherever a label is read.
func NormalizeLabel(label string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(label)), LabelPrefix)
}

// Profile holds infrastructure-specific settings loaded from E2E_PROFILE_YAML.
// JSON tags are required by sigs.k8s.io/yaml.
type Profile struct {
	NebiusProjectID            string           `json:"nebius_project_id"`
	NebiusRegion               string           `json:"nebius_region"`
	NebiusTenantID             string           `json:"nebius_tenant_id"`
	NebiusProfile              string           `json:"nebius_profile,omitempty"`
	TerraformBackendS3Endpoint string           `json:"terraform_backend_s3_endpoint,omitempty"`
	CapacityStrategy           CapacityStrategy `json:"capacity_strategy"`
	Labels                     []string         `json:"labels"`
	NodeSets                   []NodeSetDef     `json:"nodesets"`
}

// HasLabel reports whether the profile carries the given label.
func (p *Profile) HasLabel(label string) bool {
	return slices.Contains(p.Labels, label)
}

// Validate checks that the profile is well-formed.
func (p *Profile) Validate() error {
	switch p.CapacityStrategy {
	case "":
		p.CapacityStrategy = CapacityStrategyWarn
	case CapacityStrategyWarn, CapacityStrategyCancel:
		// ok
	default:
		return fmt.Errorf("unknown capacity_strategy %q (valid: warn, cancel)", p.CapacityStrategy)
	}

	seenLabels := make(map[string]struct{}, len(p.Labels))
	for i, label := range p.Labels {
		// Normalized so a stray capital or space cannot silently drop a profile out of automatic selection.
		normalized := NormalizeLabel(label)
		if normalized == "" {
			return fmt.Errorf("labels[%d]: must not be empty", i)
		}
		if !labelRe.MatchString(normalized) {
			return fmt.Errorf("labels[%d]: %q must match %s", i, label, labelRe)
		}
		if _, ok := seenLabels[normalized]; ok {
			return fmt.Errorf("labels[%d]: duplicate label %q", i, label)
		}
		seenLabels[normalized] = struct{}{}
		p.Labels[i] = normalized
	}

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

// ParseProfile unmarshals and validates a single profile from its YAML body.
func ParseProfile(raw string) (Profile, error) {
	var p Profile
	if err := yaml.Unmarshal([]byte(raw), &p); err != nil {
		return Profile{}, fmt.Errorf("unmarshal profile YAML: %w", err)
	}

	if err := p.Validate(); err != nil {
		return Profile{}, fmt.Errorf("validate profile: %w", err)
	}

	return p, nil
}

// YAML renders the profile back into the form the E2E_PROFILE_YAML env var carries.
func (p *Profile) YAML() (string, error) {
	data, err := yaml.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("marshal profile YAML: %w", err)
	}
	return string(data), nil
}

// LoadProfile reads the E2E_PROFILE_YAML env var (already-resolved YAML content) and returns a Profile.
func LoadProfile() (Profile, error) {
	raw := os.Getenv("E2E_PROFILE_YAML")
	if raw == "" {
		return Profile{}, fmt.Errorf("E2E_PROFILE_YAML env var is not set")
	}

	return ParseProfile(raw)
}

// SchedulerSettings caps how many e2e runs the scheduler workflow starts.
// That workflow is plain bash and reads these with yq — go code doesn't use it.
// They are declared anyway so that a malformed value fails config load.
type SchedulerSettings struct {
	RunsPerTick int `json:"runs_per_tick"`
	MaxInFlight int `json:"max_in_flight"`
}

// SelectorSettings drives how long SelectProfile waits for a candidate to become free.
// Which profiles are candidates is decided by the labels each profile carries, not in Go code.
type SelectorSettings struct {
	TimeoutMinutes int `json:"timeout_minutes"`
	PollSeconds    int `json:"poll_seconds"`
}

// Zero values fall back to these, so an operator only has to spell out what they want to change.
const (
	// Keep in sync with the yq fallbacks in e2e_test_scheduler.yml.
	defaultRunsPerTick = 1
	defaultMaxInFlight = 2

	defaultTimeoutMinutes = 60
	defaultPollSeconds    = 60
)

func (s *SchedulerSettings) applyDefaults() {
	if s.RunsPerTick <= 0 {
		s.RunsPerTick = defaultRunsPerTick
	}
	if s.MaxInFlight <= 0 {
		s.MaxInFlight = defaultMaxInFlight
	}
}

func (s *SelectorSettings) applyDefaults() {
	if s.TimeoutMinutes <= 0 {
		s.TimeoutMinutes = defaultTimeoutMinutes
	}
	if s.PollSeconds <= 0 {
		s.PollSeconds = defaultPollSeconds
	}
}

// E2EConfig is the whole e2e configuration: every known profile, plus the settings of the two things that act on them.
type E2EConfig struct {
	Scheduler SchedulerSettings  `json:"scheduler"`
	Selector  SelectorSettings   `json:"selector"`
	Profiles  map[string]Profile `json:"profiles"`
}

var profileNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// NormalizeProfileName maps the ways a profile can be spelled onto its config key:
// "man_h100" and "MAN_H100" both become "MAN_H100".
func NormalizeProfileName(name string) string {
	return strings.ToUpper(strings.TrimSpace(name))
}

// Names returns every configured profile name, sorted, for error messages and logs.
func (c *E2EConfig) Names() []string {
	names := make([]string, 0, len(c.Profiles))
	for name := range c.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Labels returns every label in use across the configured profiles, sorted.
func (c *E2EConfig) Labels() []string {
	seen := make(map[string]struct{})
	for _, profile := range c.Profiles {
		for _, label := range profile.Labels {
			seen[label] = struct{}{}
		}
	}

	labels := make([]string, 0, len(seen))
	for label := range seen {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

// Candidates returns the profiles carrying the given label, sorted so that the
// order the selector reports is stable across runs.
func (c *E2EConfig) Candidates(label string) []string {
	var names []string
	for _, name := range c.Names() {
		profile := c.Profiles[name]
		if profile.HasLabel(label) {
			names = append(names, name)
		}
	}
	return names
}

// Validate checks that the configuration is well-formed and fills in defaults.
func (c *E2EConfig) Validate() error {
	if len(c.Profiles) == 0 {
		return fmt.Errorf("profiles must not be empty")
	}

	// Iterate in sorted order so a broken config always reports the same profile first.
	for _, name := range c.Names() {
		if !profileNameRe.MatchString(name) {
			return fmt.Errorf("profile %q: name must match %s", name, profileNameRe)
		}
		p := c.Profiles[name]
		if err := p.Validate(); err != nil {
			return fmt.Errorf("profile %q: %w", name, err)
		}
		// Validate defaults capacity_strategy in place, and map values are copies.
		c.Profiles[name] = p
	}

	c.Scheduler.applyDefaults()
	c.Selector.applyDefaults()

	return nil
}

// LoadE2EConfig reads the E2E_CONFIG env var (the whole e2e config as YAML).
func LoadE2EConfig() (E2EConfig, error) {
	raw := os.Getenv("E2E_CONFIG")
	if raw == "" {
		return E2EConfig{}, fmt.Errorf("E2E_CONFIG env var is not set")
	}

	var c E2EConfig
	if err := yaml.Unmarshal([]byte(raw), &c); err != nil {
		return E2EConfig{}, fmt.Errorf("unmarshal e2e config YAML: %w", err)
	}

	if err := c.Validate(); err != nil {
		return E2EConfig{}, fmt.Errorf("validate e2e config: %w", err)
	}

	return c, nil
}

// Config is what one e2e run needs at execution time, read from the environment
// the workflow sets up. Distinct from [E2EConfig], which describes the profiles a run
// can choose between.
// TODO: Rename to RunConfig
//
//nolint:tagalign
type Config struct {
	SoperatorVersion   string `split_words:"true" required:"true"`                // SOPERATOR_VERSION
	SoperatorUnstable  bool   `split_words:"true" required:"true"`                // SOPERATOR_UNSTABLE
	RunUnstableTests   bool   `split_words:"true" default:"false"`                // RUN_UNSTABLE_TESTS
	PathToInstallation string `split_words:"true" required:"true"`                // PATH_TO_INSTALLATION
	O11yAccessToken    string `split_words:"true" required:"true"`                // O11Y_ACCESS_TOKEN
	O11ySecretName     string `split_words:"true" default:"o11y-writer-sa-token"` // O11Y_SECRET_NAME
	O11yNamespace      string `split_words:"true" default:"logs-system"`          // O11Y_NAMESPACE
	SlurmClusterName   string `split_words:"true" default:"soperator"`            // SLURM_CLUSTER_NAME

	Profile      Profile `ignored:"true"`
	SSHPublicKey string  `ignored:"true"`
}
