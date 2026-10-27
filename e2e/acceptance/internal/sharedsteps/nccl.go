package sharedsteps

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	ncclWorkerCount      = 2
	ncclImmediateTimeout = 120 * time.Second
	ncclCommandTimeout   = 6 * time.Minute
	ncclSlurmTimeLimit   = "3:00"
	ncclOutputFilename   = "nccl-all-reduce.log"
	ncclExecutable       = "soperator-nccl-test"
)

var (
	ncclPlatformLine  = regexp.MustCompile(`^Detected platform: (H100|H200|B200|B300|GB300)$`)
	ncclCorrectLine   = regexp.MustCompile(`^#\s*Out of bounds values\s*:\s*0\s+OK\s*$`)
	ncclBandwidthLine = regexp.MustCompile(
		`^#\s*Avg bus bandwidth\s*:\s*(\S+)\s*$`,
	)
)

type NCCL struct {
	runtime  framework.Runtime
	selector *framework.WorkerSelector

	workers []framework.WorkerInfo
	output  string
	runErr  error
}

type ncclResult struct {
	Platform     string
	AvgBandwidth float64
}

func NewNCCL(runtime framework.Runtime, selector *framework.WorkerSelector) *NCCL {
	return &NCCL{
		runtime:  runtime,
		selector: selector,
	}
}

func (s *NCCL) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^two idle GPU workers are available for NCCL validation$`, s.selectIdleGPUWorkers)
	sc.Step(`^the jail NCCL utility runs a bounded all-reduce test on them$`, s.runNCCLTest)
	sc.Step(`^the NCCL result is correct$`, s.validateNCCLResult)
}

func (s *NCCL) CleanupAndReset(context.Context) {
	s.workers = nil
	s.output = ""
	s.runErr = nil
}

func (s *NCCL) selectIdleGPUWorkers(ctx context.Context) error {
	workers, err := s.selector.Acquire(ctx, framework.WorkerRequirements{Kind: "GPU", Count: ncclWorkerCount, Idle: true})
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.workers = workers
	s.runtime.Logf("NCCL: selected workers=%s", strings.Join(framework.WorkerNames(workers), ","))
	return nil
}

func (s *NCCL) runNCCLTest(ctx context.Context) error {
	if len(s.workers) != ncclWorkerCount {
		return fmt.Errorf("select %d idle GPU workers for NCCL validation", ncclWorkerCount)
	}

	command := ncclTestCommand(s.workers)
	s.output, s.runErr = s.runtime.Jail().Run(ctx, command)
	if err := writeNCCLOutputArtifact(ctx, s.output); err != nil {
		return err
	}
	s.runtime.Logf("NCCL: command=%s output_artifact=%s", command, ncclOutputFilename)
	return nil
}

func (s *NCCL) validateNCCLResult() error {
	workerNames := strings.Join(framework.WorkerNames(s.workers), ",")
	if s.runErr != nil {
		return fmt.Errorf("run NCCL all-reduce on workers %s: %w; output:\n%s",
			workerNames, s.runErr, strings.TrimSpace(s.output))
	}

	result, err := parseNCCLResult(s.output)
	if err != nil {
		return fmt.Errorf("validate NCCL all-reduce output from workers %s: %w; output:\n%s",
			workerNames, err, strings.TrimSpace(s.output))
	}
	s.runtime.Logf("NCCL: platform=%s avg_bus_bandwidth=%g", result.Platform, result.AvgBandwidth)
	return nil
}

func pickIdleGPUWorkers(snapshot framework.WorkerSnapshot) ([]framework.WorkerInfo, error) {
	var configured int
	for _, nodeSet := range snapshot.NodeSets {
		if nodeSet.HasGPU && nodeSet.Replicas > 0 {
			configured += nodeSet.Replicas
		}
	}
	if configured < ncclWorkerCount {
		return nil, &framework.InsufficientWorkersError{
			Label: "GPU workers",
			Found: configured,
			Need:  ncclWorkerCount,
		}
	}

	var idle []framework.WorkerInfo
	var unavailable []string
	for _, worker := range snapshot.GPUWorkers {
		if worker.SlurmNode.IsReadyForImmediateWork() && worker.SlurmNode.HasStateFlag("IDLE") {
			idle = append(idle, worker)
			continue
		}
		unavailable = append(unavailable, fmt.Sprintf("%s state=%s reason=%s",
			worker.Name, worker.SlurmNode.State, worker.SlurmNode.Reason))
	}
	if len(idle) < ncclWorkerCount {
		if missing := configured - len(snapshot.GPUWorkers); missing > 0 {
			unavailable = append(unavailable, fmt.Sprintf("%d configured GPU workers missing from Slurm", missing))
		}
		sort.Strings(unavailable)
		return nil, fmt.Errorf("found %d idle GPU workers, need %d (%d configured, %d discovered): %s",
			len(idle), ncclWorkerCount, configured, len(snapshot.GPUWorkers), strings.Join(unavailable, "; "))
	}

	sort.Slice(idle, func(i, j int) bool {
		return idle[i].Name < idle[j].Name
	})
	return idle[:ncclWorkerCount], nil
}

func ncclTestCommand(workers []framework.WorkerInfo) string {
	nodelist := strings.Join(framework.WorkerNames(workers), ",")
	args := []string{
		framework.ShellQuote(ncclExecutable),
		fmt.Sprintf("--nodes=%d", len(workers)),
		"--nodelist=" + framework.ShellQuote(nodelist),
		fmt.Sprintf("--immediate=%d", int(ncclImmediateTimeout.Seconds())),
		"--min-message-size=512M",
		"--max-message-size=1G",
		"--message-step-factor=2",
		"--time-limit=" + ncclSlurmTimeLimit,
	}
	return fmt.Sprintf("timeout --kill-after=30s %d %s 2>&1",
		int(ncclCommandTimeout.Seconds()), strings.Join(args, " "))
}

func writeNCCLOutputArtifact(ctx context.Context, output string) error {
	artifacts, ok := framework.ScenarioArtifacts(ctx)
	if !ok {
		return nil
	}
	artifactPath := filepath.Join(artifacts.RunnerDir, ncclOutputFilename)
	if err := os.WriteFile(artifactPath, []byte(output), 0o644); err != nil {
		return fmt.Errorf("write NCCL output artifact %q: %w", artifactPath, err)
	}
	return nil
}

func parseNCCLResult(output string) (ncclResult, error) {
	var platforms []string
	var correctLines int
	var bandwidthValues []string

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if match := ncclPlatformLine.FindStringSubmatch(line); len(match) == 2 {
			platforms = append(platforms, match[1])
		}
		if ncclCorrectLine.MatchString(line) {
			correctLines++
		}
		if match := ncclBandwidthLine.FindStringSubmatch(line); len(match) == 2 {
			bandwidthValues = append(bandwidthValues, match[1])
		}
	}
	if err := scanner.Err(); err != nil {
		return ncclResult{}, fmt.Errorf("scan output: %w", err)
	}

	if len(platforms) != 1 {
		return ncclResult{}, fmt.Errorf("expected one supported platform line, found %d", len(platforms))
	}
	if correctLines != 1 {
		return ncclResult{}, fmt.Errorf("expected one zero out-of-bounds success line, found %d", correctLines)
	}
	if len(bandwidthValues) != 1 {
		return ncclResult{}, fmt.Errorf("expected one average bus bandwidth line, found %d", len(bandwidthValues))
	}

	bandwidth, err := strconv.ParseFloat(bandwidthValues[0], 64)
	if err != nil {
		return ncclResult{}, fmt.Errorf("parse average bus bandwidth %q: %w", bandwidthValues[0], err)
	}
	if bandwidth <= 0 || math.IsNaN(bandwidth) || math.IsInf(bandwidth, 0) {
		return ncclResult{}, fmt.Errorf("expected finite positive average bus bandwidth, got %q", bandwidthValues[0])
	}

	return ncclResult{Platform: platforms[0], AvgBandwidth: bandwidth}, nil
}
