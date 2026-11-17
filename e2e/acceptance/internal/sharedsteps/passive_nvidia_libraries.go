package sharedsteps

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const nvidiaLibrariesCheckName = "nvidia_libraries"

var nvidiaLibrariesLoadedPattern = regexp.MustCompile(`(?m)^All [1-9][0-9]* NVIDIA libraries are loadable\r?$`)

func (s *PassiveChecks) nvidiaLibrariesCheckIsEnabled(ctx context.Context) error {
	pod, err := framework.NewKubectlClient(s.runtime).WorkerPodForSlurmNode(ctx, s.gpuWorker.Name)
	if err != nil {
		return err
	}
	metadata, err := s.runtime.WorkerPod(pod).RunWithDefaultRetry(ctx, "cat /run/soperator/node_metadata.env")
	if err != nil {
		return fmt.Errorf("read passive-check platform metadata on %s: %w", s.gpuWorker.Name, err)
	}
	var platformTags []string
	for _, line := range strings.Split(metadata, "\n") {
		if tags, ok := strings.CutPrefix(line, "SOPERATOR_NODE_PLATFORM_TAGS="); ok && strings.TrimSpace(tags) != "" {
			platformTags = strings.Split(strings.TrimSpace(tags), ",")
		}
	}
	if len(platformTags) == 0 {
		return fmt.Errorf("read passive-check platform tags on %s: missing metadata", s.gpuWorker.Name)
	}
	config, err := s.runtime.Worker(s.gpuWorker).RunWithDefaultRetry(ctx, "cat /opt/slurm_scripts/checks.json")
	if err != nil {
		return fmt.Errorf("read passive-check configuration on %s: %w", s.gpuWorker.Name, err)
	}
	enabled, err := nvidiaLibrariesCheckEnabled(config, platformTags)
	if err != nil {
		return err
	}
	if !enabled {
		s.runtime.Logf("NVIDIA libraries check is disabled or inapplicable on %s, skipping scenario", s.gpuWorker.Name)
		return godog.ErrSkip
	}
	return assertHealthCheckProgramConfigured(ctx, s.runtime)
}

func nvidiaLibrariesCheckEnabled(config string, platformTags []string) (bool, error) {
	var checks []struct {
		Name       string   `json:"name"`
		Contexts   []string `json:"contexts"`
		Platforms  []string `json:"platforms"`
		NodeStates []string `json:"node_states"`
	}
	if err := json.Unmarshal([]byte(config), &checks); err != nil {
		return false, fmt.Errorf("parse passive-check configuration: %w", err)
	}
	if checks == nil {
		return false, fmt.Errorf("parse passive-check configuration: expected an array of checks")
	}
	for _, check := range checks {
		if check.Name != nvidiaLibrariesCheckName {
			continue
		}
		if check.Contexts != nil && !slices.Contains(check.Contexts, "any") &&
			(!slices.Contains(check.Contexts, "prolog") || !slices.Contains(check.Contexts, "hc_program")) {
			continue
		}
		if check.NodeStates != nil && !slices.Contains(check.NodeStates, "any") {
			continue
		}
		if check.Platforms == nil || slices.Contains(check.Platforms, "any") || slices.ContainsFunc(platformTags, func(tag string) bool {
			return slices.Contains(check.Platforms, tag)
		}) {
			return true, nil
		}
	}
	return false, nil
}

func (s *PassiveChecks) nvidiaLibrariesPrologCheckPasses(ctx context.Context) error {
	output, err := waitForHealthyCheckRunnerOutputForJob(ctx, s.runtime, s.gpuWorker,
		checkRunnerOutputPath(s.info.TargetSoperatorVersion, s.gpuWorker.Name, "prolog"), s.gpuHooksJob.ID, passiveGPUJobTimeout)
	if err != nil {
		return err
	}
	return s.assertNVIDIALibrariesCheckOutput(ctx, "prolog", output)
}

func (s *PassiveChecks) runFreshNVIDIAHealthCheckProgram(ctx context.Context) error {
	runCtx, cancel := context.WithTimeout(ctx, passiveHCProgramTimeout)
	defer cancel()
	output, _, err := readWorkerFileIfExists(runCtx, s.runtime, s.gpuWorker,
		checkRunnerOutputPath(s.info.TargetSoperatorVersion, s.gpuWorker.Name, "hc_program"))
	if err != nil {
		return fmt.Errorf("read previous HealthCheckProgram output: %w", err)
	}
	s.nvidiaHCProgramBaseline = output
	return runManualHCProgram(runCtx, s.runtime, s.gpuWorker)
}

func (s *PassiveChecks) nvidiaLibrariesHCProgramCheckPasses(ctx context.Context) error {
	output, err := waitForHealthyCheckRunnerOutputForJob(ctx, s.runtime, s.gpuWorker,
		checkRunnerOutputPath(s.info.TargetSoperatorVersion, s.gpuWorker.Name, "hc_program"), "", passiveHCProgramTimeout)
	if err != nil {
		return err
	}
	if output == s.nvidiaHCProgramBaseline {
		return fmt.Errorf("verify fresh HealthCheckProgram output on %s: runner output is unchanged", s.gpuWorker.Name)
	}
	return s.assertNVIDIALibrariesCheckOutput(ctx, "hc_program", output)
}

func (s *PassiveChecks) assertNVIDIALibrariesCheckOutput(ctx context.Context, hook, runnerOutput string) error {
	path := fmt.Sprintf("%s/%s.%s.%s.out", slurmScriptsOutputDir(s.info.TargetSoperatorVersion), s.gpuWorker.Name, nvidiaLibrariesCheckName, hook)
	output, _, err := readWorkerFileIfExists(ctx, s.runtime, s.gpuWorker, path)
	if artifacts, ok := framework.ScenarioArtifacts(ctx); ok {
		for name, content := range map[string]string{"runner": runnerOutput, "check": output} {
			if writeErr := artifacts.WriteRunnerFile(nvidiaLibrariesCheckName+"-"+hook+"-"+name+".log", content); writeErr != nil {
				s.runtime.Logf("Write NVIDIA libraries artifact: %v", writeErr)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("read %s NVIDIA libraries log on %s: %w", hook, s.gpuWorker.Name, err)
	}
	if err := assertCheckRunnerCheckOK(runnerOutput, nvidiaLibrariesCheckName); err != nil {
		return err
	}
	if err := assertNVIDIALibrariesLoaded(output); err != nil {
		return fmt.Errorf("verify %s NVIDIA libraries check on %s: %w", hook, s.gpuWorker.Name, err)
	}
	return nil
}

func assertNVIDIALibrariesLoaded(output string) error {
	var cudaLoaded, nvmlLoaded bool
	for _, line := range strings.Split(output, "\n") {
		switch strings.TrimSpace(line) {
		case "Loaded libcuda.so.1":
			cudaLoaded = true
		case "Loaded libnvidia-ml.so.1":
			nvmlLoaded = true
		}
	}
	if !cudaLoaded || !nvmlLoaded || !nvidiaLibrariesLoadedPattern.MatchString(output) ||
		strings.Contains(strings.ToLower(output), "skipping") || strings.Contains(output, "Unhandled exception") {
		return fmt.Errorf("require completed NVIDIA library loading without skipped checks, got:\n%s", strings.TrimSpace(output))
	}
	return nil
}
