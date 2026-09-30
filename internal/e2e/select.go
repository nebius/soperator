package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nebius/gosdk"
	"github.com/nebius/gosdk/config/reader"
)

// ErrNoProfileAvailable means no configured profile satisfied both constraints before the selection deadline.
var ErrNoProfileAvailable = errors.New("no e2e profile became available")

const (
	// e2eWorkflowFile is the workflow whose runs compete for the same projects.
	e2eWorkflowFile = "e2e_test.yml"
	// runMetadataArtifact is where a run publishes the profile it took, so that
	// other runs can see the project as occupied before any instance exists.
	runMetadataArtifact = "e2e-run-metadata"
	runMetadataFile     = "e2e-run-metadata.json"
)

// RunClaim is one run's stake on a Nebius project, published as an artifact right after the run resolves its profile.
type RunClaim struct {
	RunID       int64  `json:"run_id"`
	ProfileName string `json:"profile_name"`
	ProjectID   string `json:"project_id"`
	Region      string `json:"region"`
	IsScheduled bool   `json:"is_scheduled"`
}

// WriteRunMetadata writes this run's claim to path for upload as an artifact.
func WriteRunMetadata(path string, claim RunClaim) error {
	data, err := json.Marshal(claim)
	if err != nil {
		return fmt.Errorf("marshal run metadata: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write run metadata to %s: %w", path, err)
	}
	return nil
}

// CurrentRunID returns this GitHub Actions run's id, or 0 outside Actions.
func CurrentRunID() int64 {
	id, err := strconv.ParseInt(os.Getenv("GITHUB_RUN_ID"), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// activeRunIDs lists e2e runs that have not finished yet, excluding this one.
// Queued runs count: a run waiting on the concurrency mutex still owns its project.
func activeRunIDs(ctx context.Context, repo string) ([]int64, error) {
	cmd := exec.CommandContext(ctx, "gh", "api",
		fmt.Sprintf("repos/%s/actions/workflows/%s/runs?per_page=100", repo, e2eWorkflowFile),
		"--jq", `.workflow_runs[] | select(.status != "completed") | .id`,
	)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list active e2e runs: %w", err)
	}

	current := CurrentRunID()
	var ids []int64
	for _, line := range strings.Fields(string(out)) {
		id, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse run id %q: %w", line, err)
		}
		if id == current {
			continue
		}
		ids = append(ids, id)
	}

	return ids, nil
}

// downloadClaim reads another run's claim. A run that has not published one yet is
// reported as absent rather than as an error — it simply has not picked a profile.
func downloadClaim(ctx context.Context, repo string, runID int64) (claim RunClaim, ok bool) {
	dir, err := os.MkdirTemp("", "e2e-claim-")
	if err != nil {
		log.Printf("Run %d: create temp dir: %v", runID, err)
		return RunClaim{}, false
	}
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	cmd := exec.CommandContext(ctx, "gh", "run", "download", strconv.FormatInt(runID, 10),
		"-n", runMetadataArtifact, "-D", dir, "-R", repo)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		log.Printf("Run %d: no %s artifact yet, treating as unclaimed", runID, runMetadataArtifact)
		return RunClaim{}, false
	}

	data, err := os.ReadFile(filepath.Join(dir, runMetadataFile))
	if err != nil {
		log.Printf("Run %d: read run metadata: %v", runID, err)
		return RunClaim{}, false
	}

	if err := json.Unmarshal(data, &claim); err != nil {
		log.Printf("Run %d: parse run metadata: %v", runID, err)
		return RunClaim{}, false
	}
	claim.RunID = runID

	return claim, true
}

// ActiveClaims collects the projects currently held by other e2e runs.
func ActiveClaims(ctx context.Context) ([]RunClaim, error) {
	repo := os.Getenv("GITHUB_REPOSITORY")
	if repo == "" {
		return nil, fmt.Errorf("GITHUB_REPOSITORY env var is not set")
	}

	ids, err := activeRunIDs(ctx, repo)
	if err != nil {
		return nil, err
	}

	var claims []RunClaim
	for _, id := range ids {
		claim, ok := downloadClaim(ctx, repo, id)
		if !ok {
			continue
		}
		// A run started before profile names were published reports its project
		// only. That still excludes the project, which is the binding constraint.
		name := claim.ProfileName
		if name == "" {
			name = "unnamed"
		}
		log.Printf("Run %d holds project %s (profile %s)", claim.RunID, claim.ProjectID, name)
		claims = append(claims, claim)
	}

	return claims, nil
}

// capacityOracle answers whether a profile's demand fits right now.
// It is a parameter so the selection logic can be exercised without the capacity API.
type capacityOracle func(profile Profile, reserved map[affinityKey]uint64) (bool, error)

// reservedDemand sums the GPU demand of the profiles other runs have claimed. Those
// runs may not have created their instances yet, so their demand is invisible in the
// capacity block usage. A claim whose instances already exist is counted twice, which
// is deliberate — the selector would rather leave a busy block alone than crowd it.
//
// Claims naming a profile this config no longer knows contribute nothing; their
// project is still excluded, and once their instances exist the usage covers them.
func reservedDemand(cfg E2EConfig, claims []RunClaim) map[affinityKey]uint64 {
	reserved := make(map[affinityKey]uint64)
	for _, claim := range claims {
		profile, ok := cfg.Profiles[claim.ProfileName]
		if !ok {
			continue
		}
		demands, _ := gpuDemands(profile)
		for key, demand := range demands {
			reserved[key] += demand.requiredGPUs
		}
	}
	return reserved
}

// filterFeasible returns the candidates that satisfy both constraints: nobody else
// holds their project, and their capacity block has room.
func filterFeasible(cfg E2EConfig, candidates []string, claims []RunClaim, oracle capacityOracle) ([]string, error) {
	// Keyed by project, not by profile name: several profiles can share one project
	// and one concurrency group, so claiming any of them takes all of them.
	occupied := make(map[string]int64, len(claims))
	for _, claim := range claims {
		occupied[claim.ProjectID] = claim.RunID
	}
	reserved := reservedDemand(cfg, claims)

	var feasible []string
	for _, name := range candidates {
		profile := cfg.Profiles[name]

		if runID, taken := occupied[profile.NebiusProjectID]; taken {
			log.Printf("Profile %s: project %s is held by run %d", name, profile.NebiusProjectID, runID)
			continue
		}

		ok, err := oracle(profile, reserved)
		if err != nil {
			return nil, err
		}
		if !ok {
			log.Printf("Profile %s: not enough capacity", name)
			continue
		}

		log.Printf("Profile %s: available", name)
		feasible = append(feasible, name)
	}

	return feasible, nil
}

type selectOptions struct {
	timeout time.Duration
	poll    time.Duration
	claims  func(context.Context) ([]RunClaim, error)
	oracle  capacityOracle
	pick    func(n int) int
}

// candidatesFor returns the profiles the selector may pick, or an error naming the labels
// that do exist when the requested one matches nothing.
func candidatesFor(cfg E2EConfig, label string) ([]string, error) {
	candidates := cfg.Candidates(label)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no profile carries the label %q (labels in use: %s)",
			label, strings.Join(cfg.Labels(), ", "))
	}
	return candidates, nil
}

// newNebiusSDK uses the named Nebius CLI profile when one is configured for the
// E2E profile. Legacy profiles without one keep using NEBIUS_IAM_TOKEN or the
// default CLI profile. Service-account profiles keep exchanged tokens fresh,
// which matters when the selector waits longer than one token stays valid.
func newNebiusSDK(ctx context.Context, profileName string) (*gosdk.SDK, error) {
	if token := os.Getenv("NEBIUS_IAM_TOKEN"); token != "" && profileName == "" {
		sdk, err := gosdk.New(ctx, gosdk.WithCredentials(gosdk.IAMToken(token)))
		if err != nil {
			return nil, fmt.Errorf("create nebius sdk with env token: %w", err)
		}
		return sdk, nil
	}

	configReader := reader.NewConfigReader(reader.WithoutFileCache())
	if profileName != "" {
		configReader = reader.NewConfigReader(reader.WithoutFileCache(), reader.WithProfileName(profileName))
	}
	sdk, err := gosdk.New(ctx, gosdk.WithConfigReader(configReader))
	if err != nil {
		return nil, fmt.Errorf("create nebius sdk for profile %q (needs ~/.nebius/config.yaml or NEBIUS_IAM_TOKEN): %w",
			profileName, err)
	}
	return sdk, nil
}

// SelectProfile waits until one of the profiles carrying the given label is free  and returns it.
func SelectProfile(ctx context.Context, cfg E2EConfig, label string) (name string, profile Profile, err error) {
	// Checked before authenticating, so a mistyped label fails immediately rather than behind a credentials error.
	if _, err := candidatesFor(cfg, label); err != nil {
		return "", Profile{}, err
	}

	sdks := make(map[string]*gosdk.SDK)
	defer func() {
		for _, sdk := range sdks {
			_ = sdk.Close()
		}
	}()

	return selectProfile(ctx, cfg, label, selectOptions{
		timeout: time.Duration(cfg.Selector.TimeoutMinutes) * time.Minute,
		poll:    time.Duration(cfg.Selector.PollSeconds) * time.Second,
		claims:  ActiveClaims,
		oracle: func(profile Profile, reserved map[affinityKey]uint64) (bool, error) {
			sdk, ok := sdks[profile.NebiusProfile]
			if !ok {
				var err error
				sdk, err = newNebiusSDK(ctx, profile.NebiusProfile)
				if err != nil {
					return false, err
				}
				sdks[profile.NebiusProfile] = sdk
			}
			return hasCapacity(ctx, sdk, profile, reserved)
		},
		pick: rand.IntN,
	})
}

func selectProfile(ctx context.Context, cfg E2EConfig, label string, opts selectOptions) (name string, profile Profile, err error) {
	candidates, err := candidatesFor(cfg, label)
	if err != nil {
		return "", Profile{}, err
	}

	deadline := time.Now().Add(opts.timeout)

	for attempt := 1; ; attempt++ {
		claims, err := opts.claims(ctx)
		if err != nil {
			return "", Profile{}, err
		}
		log.Printf("Attempt %d: %d other e2e run(s) in flight, checking %s",
			attempt, len(claims), strings.Join(candidates, ", "))

		feasible, err := filterFeasible(cfg, candidates, claims, opts.oracle)
		if err != nil {
			return "", Profile{}, err
		}

		if len(feasible) > 0 {
			// Pick at random rather than best-fit: two selectors running at the same
			// moment see the same state, and a deterministic choice would send both
			// to the same project.
			name = feasible[opts.pick(len(feasible))]
			log.Printf("Selected profile %s out of %d available (%s)",
				name, len(feasible), strings.Join(feasible, ", "))
			return name, cfg.Profiles[name], nil
		}

		if !time.Now().Add(opts.poll).Before(deadline) {
			return "", Profile{}, fmt.Errorf("%w within %s (candidates: %s)",
				ErrNoProfileAvailable, opts.timeout, strings.Join(candidates, ", "))
		}

		log.Printf("No profile available, retrying in %s (giving up in %s)",
			opts.poll, time.Until(deadline).Round(time.Second))

		select {
		case <-ctx.Done():
			return "", Profile{}, ctx.Err()
		case <-time.After(opts.poll):
		}
	}
}
