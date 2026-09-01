package sharedsteps

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	soperatorUtilsDir                  = "/opt/soperator_utils"
	soperatorUtilsRemoteReportPath     = "/tmp/nvidia-bug-report.log.gz"
	soperatorUtilsInstanceLoginTimeout = time.Minute
	soperatorUtilsTaskTimeout          = 2 * time.Minute
)

type fsUsageMode struct {
	Flag         string
	AllowedTypes []string
}

var soperatorUtilsFSUsageModes = []fsUsageMode{
	{AllowedTypes: []string{"virtiofs", "tmpfs", "nfs4", "overlay", "ext4"}},
	{Flag: "-s", AllowedTypes: []string{"virtiofs", "nfs4"}},
	{Flag: "-l", AllowedTypes: []string{"overlay", "ext4"}},
	{Flag: "-m", AllowedTypes: []string{"tmpfs"}},
}

var soperatorUtilsFSUsageHeader = []string{"Size", "Use%", "FSType", "Directory"}

var soperatorUtilsKubeletProcPathPattern = regexp.MustCompile(`(?m)^/proc/[0-9]+/comm$`)

type SoperatorUtils struct {
	runtime              framework.Runtime
	selector             *framework.WorkerSelector
	worker               framework.WorkerInfo
	fsUsageOutputs       map[string]string
	instanceLoginOutputs map[string]string
	taskInfoOutput       string
	reportPath           string
	reportAttempted      bool
}

func NewSoperatorUtils(runtime framework.Runtime, selector *framework.WorkerSelector) *SoperatorUtils {
	return &SoperatorUtils{runtime: runtime, selector: selector}
}

func (s *SoperatorUtils) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a worker is selected for Soperator utility checks$`, s.selectWorker)
	sc.Step(`^a GPU worker is selected for Soperator utility checks$`, s.selectGPUWorker)
	sc.Step(`^fs_usage runs without a filter and with every supported filter$`, s.runFSUsageModes)
	sc.Step(`^every fs_usage result contains only its expected filesystem types$`, s.checkFSUsageResults)
	sc.Step(`^soperator_instance_login queries the worker host by name and instance ID$`, s.queryWorkerHostByNameAndInstanceID)
	sc.Step(`^both queries find the host kubelet process$`, s.checkInstanceLoginResults)
	sc.Step(`^slurm_task_info runs as the prolog of a one-GPU task$`, s.runTaskInfoProlog)
	sc.Step(`^task info reports the selected node, rank, CPU, GPU, and CUDA device$`, s.checkTaskInfo)
	sc.Step(`^worker_nvidia_bug_report downloads a report by instance ID$`, s.downloadNVIDIABugReport)
	sc.Step(`^the report is a non-empty valid gzip file$`, s.checkNVIDIABugReport)
}

func (s *SoperatorUtils) CleanupAndReset(ctx context.Context) {
	if s.reportAttempted && s.worker.Name != "" {
		if _, err := s.runtime.Worker(s.worker).Run(ctx, fmt.Sprintf("rm -f %s", framework.ShellQuote(soperatorUtilsRemoteReportPath))); err != nil {
			s.runtime.Logf("cleanup: remove NVIDIA bug report from worker %s: %v", s.worker.Name, err)
		}
	}
	if s.reportPath != "" {
		if _, err := s.runtime.Jail().Run(ctx, fmt.Sprintf("rm -f %s", framework.ShellQuote(s.reportPath))); err != nil {
			s.runtime.Logf("cleanup: remove downloaded NVIDIA bug report %s: %v", s.reportPath, err)
		}
	}

	s.worker = framework.WorkerInfo{}
	s.fsUsageOutputs = nil
	s.instanceLoginOutputs = nil
	s.taskInfoOutput = ""
	s.reportPath = ""
	s.reportAttempted = false
}

func (s *SoperatorUtils) selectWorker(ctx context.Context) error {
	workers, err := s.selector.PickWorkers(ctx, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]
	return nil
}

func (s *SoperatorUtils) selectGPUWorker(ctx context.Context) error {
	workers, err := s.selector.PickGPUWorkers(ctx, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]
	return nil
}

func (s *SoperatorUtils) runFSUsageModes(ctx context.Context) error {
	s.fsUsageOutputs = make(map[string]string, len(soperatorUtilsFSUsageModes))
	for _, mode := range soperatorUtilsFSUsageModes {
		command := path.Join(soperatorUtilsDir, "fs_usage.sh")
		if mode.Flag != "" {
			command += " " + framework.ShellQuote(mode.Flag)
		}
		output, err := s.runtime.Worker(s.worker).RunWithDefaultRetry(ctx, command)
		if err != nil {
			return fmt.Errorf("run fs_usage with filter %s: %w", fsUsageFilterLabel(mode.Flag), err)
		}
		s.fsUsageOutputs[mode.Flag] = output
	}
	return nil
}

func (s *SoperatorUtils) checkFSUsageResults() error {
	return validateFSUsageOutputs(s.fsUsageOutputs)
}

func (s *SoperatorUtils) queryWorkerHostByNameAndInstanceID(ctx context.Context) error {
	if s.worker.SlurmNode.InstanceID == "" {
		return fmt.Errorf("find instance ID for Slurm worker %s", s.worker.Name)
	}

	var queries = []struct {
		label string
		flag  string
		value string
	}{
		{label: "worker name", flag: "-w", value: s.worker.Name},
		{label: "instance ID", flag: "-i", value: s.worker.SlurmNode.InstanceID},
	}

	s.instanceLoginOutputs = make(map[string]string, len(queries))
	for _, query := range queries {
		command := fmt.Sprintf("timeout %.0f %s %s %s -c %s",
			soperatorUtilsInstanceLoginTimeout.Seconds(),
			framework.ShellQuote(path.Join(soperatorUtilsDir, "soperator_instance_login.sh")),
			query.flag,
			framework.ShellQuote(query.value),
			framework.ShellQuote(`grep -l '^kubelet$' /proc/[0-9]*/comm`),
		)
		output, err := s.runtime.Jail().RunWithRetry(ctx, command, 2, 2*time.Second)
		if err != nil {
			return fmt.Errorf("query worker host by %s: %w", query.label, err)
		}
		s.instanceLoginOutputs[query.label] = output
	}
	return nil
}

func (s *SoperatorUtils) checkInstanceLoginResults() error {
	return validateInstanceLoginOutputs(s.instanceLoginOutputs)
}

func (s *SoperatorUtils) runTaskInfoProlog(ctx context.Context) error {
	command := fmt.Sprintf(
		"timeout %.0f srun -N 1 -w %s --gpus-per-node=1 --ntasks-per-node=1 --cpus-per-task=1 --task-prolog=%s true",
		soperatorUtilsTaskTimeout.Seconds(),
		framework.ShellQuote(s.worker.Name),
		framework.ShellQuote(path.Join(soperatorUtilsDir, "slurm_task_info.sh")),
	)
	output, err := s.runtime.Jail().Run(ctx, command)
	if err != nil {
		return fmt.Errorf("run one-GPU task with slurm_task_info prolog: %w", err)
	}
	s.taskInfoOutput = output
	return nil
}

func (s *SoperatorUtils) checkTaskInfo() error {
	return validateTaskInfo(s.taskInfoOutput, s.worker.Name)
}

func (s *SoperatorUtils) downloadNVIDIABugReport(ctx context.Context) error {
	if s.worker.SlurmNode.InstanceID == "" {
		return fmt.Errorf("find instance ID for Slurm worker %s", s.worker.Name)
	}

	s.reportAttempted = true
	s.reportPath = path.Join("/tmp", s.worker.Name+"-nvidia-bug-report.log.gz")
	command := fmt.Sprintf("cd /tmp && %s -i %s",
		framework.ShellQuote(path.Join(soperatorUtilsDir, "worker_nvidia_bug_report.sh")),
		framework.ShellQuote(s.worker.SlurmNode.InstanceID),
	)
	if _, err := s.runtime.Jail().Run(ctx, command); err != nil {
		return fmt.Errorf("download NVIDIA bug report from worker %s: %w", s.worker.Name, err)
	}
	return nil
}

func (s *SoperatorUtils) checkNVIDIABugReport(ctx context.Context) error {
	if _, err := s.runtime.Jail().Run(ctx, fmt.Sprintf("test -s %s", framework.ShellQuote(s.reportPath))); err != nil {
		return fmt.Errorf("verify downloaded NVIDIA bug report is non-empty: %w", err)
	}
	if _, err := s.runtime.Jail().Run(ctx, fmt.Sprintf("gzip -t %s", framework.ShellQuote(s.reportPath))); err != nil {
		return fmt.Errorf("verify downloaded NVIDIA bug report is valid gzip: %w", err)
	}
	return nil
}

func validateFSUsageOutputs(outputs map[string]string) error {
	var problems []string
	for _, mode := range soperatorUtilsFSUsageModes {
		output, found := outputs[mode.Flag]
		if !found {
			problems = append(problems, fmt.Sprintf("%s: output is missing", fsUsageFilterLabel(mode.Flag)))
			continue
		}
		types, err := parseFSUsageOutput(output)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", fsUsageFilterLabel(mode.Flag), err))
			continue
		}
		if mode.Flag == "" && len(types) == 0 {
			problems = append(problems, "none: no filesystems reported")
			continue
		}
		for _, fsType := range types {
			if !slices.Contains(mode.AllowedTypes, fsType) {
				problems = append(problems, fmt.Sprintf("%s: filesystem type %q is not allowed", fsUsageFilterLabel(mode.Flag), fsType))
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("validate fs_usage outputs: %s", strings.Join(problems, "; "))
	}
	return nil
}

func parseFSUsageOutput(output string) ([]string, error) {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("output is empty")
	}
	if header := strings.Fields(lines[0]); !slices.Equal(header, soperatorUtilsFSUsageHeader) {
		return nil, fmt.Errorf("unexpected header %q", strings.TrimSpace(lines[0]))
	}

	var types []string
	for lineNumber, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < len(soperatorUtilsFSUsageHeader) {
			return nil, fmt.Errorf("parse row %d: expected at least 4 fields, got %d", lineNumber+2, len(fields))
		}
		types = append(types, fields[2])
	}
	return types, nil
}

func validateInstanceLoginOutputs(outputs map[string]string) error {
	var problems []string
	for _, label := range []string{"worker name", "instance ID"} {
		output, found := outputs[label]
		if !found {
			problems = append(problems, fmt.Sprintf("%s: output is missing", label))
			continue
		}
		if !soperatorUtilsKubeletProcPathPattern.MatchString(strings.TrimSpace(output)) {
			problems = append(problems, fmt.Sprintf("%s: kubelet process path is missing from %q", label, strings.TrimSpace(output)))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("validate instance login outputs: %s", strings.Join(problems, "; "))
	}
	return nil
}

func validateTaskInfo(output, expectedWorker string) error {
	info, err := parseTaskInfo(output)
	if err != nil {
		return fmt.Errorf("parse slurm_task_info output: %w", err)
	}

	var problems []string
	for _, expected := range []struct {
		key   string
		value string
	}{
		{key: "node", value: expectedWorker},
		{key: "rank", value: "0"},
	} {
		if info[expected.key] != expected.value {
			problems = append(problems, fmt.Sprintf("%s: expected %q, got %q", expected.key, expected.value, info[expected.key]))
		}
	}
	for _, key := range []string{"cpu", "gpu", "cuda_dev"} {
		if info[key] == "" {
			problems = append(problems, fmt.Sprintf("%s: value is empty", key))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("validate slurm_task_info output: %s", strings.Join(problems, "; "))
	}
	return nil
}

func parseTaskInfo(output string) (map[string]string, error) {
	var result map[string]string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "SLURM_TASK_INFO ") {
			continue
		}
		if result != nil {
			return nil, fmt.Errorf("find more than one SLURM_TASK_INFO line")
		}
		result = make(map[string]string)
		for _, field := range strings.Fields(strings.TrimPrefix(line, "SLURM_TASK_INFO ")) {
			key, value, found := strings.Cut(field, "=")
			if !found || key == "" {
				return nil, fmt.Errorf("parse field %q", field)
			}
			if _, found := result[key]; found {
				return nil, fmt.Errorf("find duplicate field %q", key)
			}
			result[key] = value
		}
	}
	if result == nil {
		return nil, fmt.Errorf("find SLURM_TASK_INFO line")
	}
	return result, nil
}

func fsUsageFilterLabel(filter string) string {
	if filter == "" {
		return "none"
	}
	return filter
}
