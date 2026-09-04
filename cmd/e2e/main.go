package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/kelseyhightower/envconfig"

	"nebius.ai/slurm-operator/internal/e2e"
)

func main() {
	log.SetFlags(0)

	if len(os.Args) < 2 {
		_, _ = fmt.Fprintf(os.Stderr, "Usage: e2e <select-profile|check-capacity|cleanup-previous|init|apply|destroy>\n")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// select-profile runs before a profile exists, so it must not load one.
	if os.Args[1] == "select-profile" {
		if err := runSelectProfile(ctx); err != nil {
			log.Fatalf("select-profile: %v", err)
		}
		return
	}

	profile, err := e2e.LoadProfile()
	if err != nil {
		log.Fatalf("Load profile: %v", err)
	}
	if err := activateNebiusProfile(profile); err != nil {
		log.Fatalf("Activate Nebius profile: %v", err)
	}

	switch os.Args[1] {
	case "check-capacity":
		err = runCheckCapacity(ctx, profile)
	case "cleanup-previous":
		cfg := loadFullConfig(profile)
		err = e2e.RunCleanupPrevious(ctx, cfg)
	case "init":
		cfg := loadFullConfig(profile)
		err = e2e.RunInit(ctx, cfg)
	case "apply":
		cfg := loadFullConfig(profile)
		err = e2e.Apply(ctx, cfg)
	case "destroy":
		cfg := loadFullConfig(profile)
		err = e2e.Destroy(ctx, cfg)
	default:
		_, _ = fmt.Fprintf(os.Stderr, "Unknown command: %s\nUsage: e2e <select-profile|check-capacity|cleanup-previous|init|apply|destroy>\n", os.Args[1])
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("%s: %v", os.Args[1], err)
	}
}

func activateNebiusProfile(profile e2e.Profile) error {
	if profile.NebiusProfile == "" {
		return nil
	}
	if err := os.Setenv("NEBIUS_PROFILE", profile.NebiusProfile); err != nil {
		return fmt.Errorf("set NEBIUS_PROFILE: %w", err)
	}
	return nil
}

func loadFullConfig(profile e2e.Profile) e2e.Config {
	var cfg e2e.Config
	if err := envconfig.Process("", &cfg); err != nil {
		log.Fatalf("Parse config: %v", err)
	}
	cfg.Profile = profile

	sshPubKey, err := e2e.GenerateSSHPublicKey()
	if err != nil {
		log.Fatalf("Generate SSH public key: %v", err)
	}
	cfg.SSHPublicKey = sshPubKey

	return cfg
}

func runCheckCapacity(ctx context.Context, profile e2e.Profile) error {
	err := e2e.CheckCapacity(ctx, profile)
	if !errors.Is(err, e2e.ErrInsufficientCapacity) {
		return err
	}

	log.Print("Insufficient capacity detected with cancel strategy, cancelling workflow")
	runID := os.Getenv("GITHUB_RUN_ID")
	if runID == "" {
		return fmt.Errorf("GITHUB_RUN_ID is not set, cannot cancel workflow")
	}

	cmd := exec.CommandContext(ctx, "gh", "run", "cancel", runID)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if cancelErr := cmd.Run(); cancelErr != nil {
		return fmt.Errorf("cancel workflow run %s: %w", runID, cancelErr)
	}

	log.Printf("Workflow run %s cancelled due to insufficient capacity", runID)
	return nil
}

const defaultRunMetadataPath = "/tmp/e2e-run-metadata.json"

// runSelectProfile decides which profile this run uses, publishes the claim so
// other runs see the project as taken, and hands the profile to the workflow.
func runSelectProfile(ctx context.Context) error {
	cfg, err := e2e.LoadE2EConfig()
	if err != nil {
		return err
	}

	name, profile, err := resolveProfile(ctx, cfg)
	if err != nil {
		return err
	}

	log.Printf("Profile %s: project=%s region=%s nebius-profile=%s",
		name, profile.NebiusProjectID, profile.NebiusRegion, profile.NebiusProfile)

	body, err := profile.YAML()
	if err != nil {
		return err
	}

	if err := writeOutputs(name, profile, body); err != nil {
		return err
	}

	metadataPath := os.Getenv("E2E_RUN_METADATA_PATH")
	if metadataPath == "" {
		metadataPath = defaultRunMetadataPath
	}
	return e2e.WriteRunMetadata(metadataPath, e2e.RunClaim{
		RunID:       e2e.CurrentRunID(),
		ProfileName: name,
		ProjectID:   profile.NebiusProjectID,
		Region:      profile.NebiusRegion,
		IsScheduled: os.Getenv("E2E_IS_SCHEDULED") == "true",
	})
}

// resolveProfile turns the request into a profile: a label selects among the
// profiles carrying it, a name picks exactly one.
func resolveProfile(ctx context.Context, cfg e2e.E2EConfig) (name string, profile e2e.Profile, err error) {
	requested := strings.TrimSpace(os.Getenv("E2E_PROFILE_NAME"))
	if requested == "" {
		// No fallback on purpose: the workflow's profile input declares the default,
		// so an empty request here means broken plumbing, not "pick anything".
		return "", e2e.Profile{}, fmt.Errorf("E2E_PROFILE_NAME env var is not set (profile name or @label)")
	}

	if label, isLabel := strings.CutPrefix(requested, e2e.LabelPrefix); isLabel {
		normalized := e2e.NormalizeLabel(label)
		if normalized == "" {
			return "", e2e.Profile{}, fmt.Errorf("request %q names an empty label", requested)
		}
		return e2e.SelectProfile(ctx, cfg, normalized)
	}

	name = e2e.NormalizeProfileName(requested)
	profile, ok := cfg.Profiles[name]
	if !ok {
		return "", e2e.Profile{}, fmt.Errorf("unknown profile %q (configured: %s)",
			requested, strings.Join(cfg.Names(), ", "))
	}

	log.Printf("Using explicitly requested profile %s", name)
	return name, profile, nil
}

func writeOutputs(name string, profile e2e.Profile, body string) error {
	outputs := [][2]string{
		{"profile_name", name},
		{"nebius_project_id", profile.NebiusProjectID},
		{"nebius_region", profile.NebiusRegion},
		{"nebius_tenant_id", profile.NebiusTenantID},
		{"nebius_profile", profile.NebiusProfile},
	}

	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		for _, kv := range outputs {
			log.Printf("%s=%s", kv[0], kv[1])
		}
		log.Printf("profile_yaml:\n%s", body)
		return nil
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open GITHUB_OUTPUT: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	for _, kv := range outputs {
		if _, err := fmt.Fprintf(f, "%s=%s\n", kv[0], kv[1]); err != nil {
			return fmt.Errorf("write output %s: %w", kv[0], err)
		}
	}
	if _, err := fmt.Fprintf(f, "profile_yaml<<E2E_PROFILE_EOF\n%s\nE2E_PROFILE_EOF\n", strings.TrimRight(body, "\n")); err != nil {
		return fmt.Errorf("write output profile_yaml: %w", err)
	}

	return nil
}
