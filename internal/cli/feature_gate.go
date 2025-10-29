package cli

import (
	"flag"
	"fmt"

	"nebius.ai/slurm-operator/internal/feature"
)

var (
	featureGates string = "none"
)

// AddFeatureGatesFlag adds a flag for the CLI to give a user ability to configure feature gates.
// Note: use before calling flag.Parse().
func AddFeatureGatesFlag() {
	flag.StringVar(&featureGates, "feature-gates", "", "A set of key=value pairs that define feature gates")
}

// ProcessFeatureGates processes feature gates passed from CLI options, and configures them within feature.Gate.
func ProcessFeatureGates() error {
	if featureGates == "" {
		return nil
	}
	if featureGates == "none" {
		panic(fmt.Errorf("feature gates are meant to be processed, but the flag was not added"))
	}

	if err := feature.Gate.Set(featureGates); err != nil {
		return fmt.Errorf("invalid feature gates: %w", err)
	}

	return nil
}
