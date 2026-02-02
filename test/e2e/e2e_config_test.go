//go:build e2e

package e2e_test

// nolint:tagalign
type testConfig struct {
	SoperatorVersion   string   `split_words:"true" required:"true"`                // SOPERATOR_VERSION
	SoperatorUnstable  bool     `split_words:"true" required:"true"`                // SOPERATOR_UNSTABLE
	PathToInstallation string   `split_words:"true" required:"true"`                // PATH_TO_INSTALLATION
	InfinibandFabric   string   `split_words:"true" required:"true"`                // INFINIBAND_FABRIC
	WorkerPlatform     string   `split_words:"true" required:"true"`                // WORKER_PLATFORM
	WorkerPreset       string   `split_words:"true" required:"true"`                // WORKER_PRESET
	SSHKeys            []string `split_words:"true" required:"true"`                // SSH_KEYS
	O11yAccessToken    string   `split_words:"true" required:"true"`                // O11Y_ACCESS_TOKEN
	O11ySecretName     string   `split_words:"true" default:"o11y-writer-sa-token"` // O11Y_SECRET_NAME
	O11yNamespace      string   `split_words:"true" default:"logs-system"`          // O11Y_NAMESPACE
	PreemptibleNodes   bool     `split_words:"true" default:"false"`                // PREEMPTIBLE_NODES
}
