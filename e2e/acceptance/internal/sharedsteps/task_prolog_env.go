package sharedsteps

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	taskPrologEnvTimeout   = 5 * time.Minute
	taskPrologEnvImmediate = 120 * time.Second
	taskPrologSSHTimeout   = 30 * time.Second

	ucxNetDevicesName = "UCX_NET_DEVICES"
	ncclIBHCAName     = "NCCL_IB_HCA"

	actualUCXNetDevicesName   = "ACTUAL_" + ucxNetDevicesName
	actualNCCLIBHCAName       = "ACTUAL_" + ncclIBHCAName
	expectedUCXNetDevicesName = "EXPECTED_" + ucxNetDevicesName
	expectedNCCLIBHCAName     = "EXPECTED_" + ncclIBHCAName

	taskPrologScriptPath          = "/opt/slurm_scripts/task_prolog.sh"
	taskPrologNCCLNetworkVarsPath = "/etc/profile.d/90-nccl-network-vars.sh"
)

type TaskPrologEnv struct {
	runtime  framework.Runtime
	selector *framework.WorkerSelector

	worker    framework.WorkerInfo
	jobOutput string
	sshOutput string
}

func NewTaskPrologEnv(runtime framework.Runtime, selector *framework.WorkerSelector) *TaskPrologEnv {
	return &TaskPrologEnv{
		runtime:  runtime,
		selector: selector,
	}
}

func (s *TaskPrologEnv) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^task-prolog environment injection is enabled$`, s.taskPrologEnvInjectionIsEnabled)
	sc.Step(`^a healthy GPU worker is selected for NCCL environment checks$`, s.selectHealthyGPUWorker)
	sc.Step(`^a GPU Slurm job launched from the login node prints its network environment$`, s.runGPUJobFromLogin)
	sc.Step(`^a regular user SSH login shell on the worker prints its network environment$`, s.runWorkerSSHLoginShell)
	sc.Step(`^the job environment contains NCCL network variables$`, s.jobEnvironmentContainsNetworkVariables)
	sc.Step(`^the worker SSH session contains NCCL network variables$`, s.workerSSHSessionContainsNetworkVariables)
}

func (s *TaskPrologEnv) CleanupAndReset(ctx context.Context) {
	s.worker = framework.WorkerInfo{}
	s.jobOutput = ""
	s.sshOutput = ""
}

func (s *TaskPrologEnv) taskPrologEnvInjectionIsEnabled(ctx context.Context) error {
	command := fmt.Sprintf(
		"grep -F %s %s >/dev/null && grep -F %s %s >/dev/null",
		framework.ShellQuote("task_prolog_env_export_vars"),
		framework.ShellQuote(taskPrologScriptPath),
		framework.ShellQuote(ucxNetDevicesName),
		framework.ShellQuote(taskPrologScriptPath),
	)
	if _, err := s.runtime.Jail().Run(ctx, command); err != nil {
		s.runtime.Logf("Task prolog env: injection is disabled, skipping scenario")
		return godog.ErrSkip
	}
	return nil
}

func (s *TaskPrologEnv) selectHealthyGPUWorker(ctx context.Context) error {
	workers, err := s.selector.PickGPUWorkers(ctx, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]
	s.runtime.Logf("Task prolog env: selected GPU worker=%s", s.worker.Name)
	return nil
}

func (s *TaskPrologEnv) runGPUJobFromLogin(ctx context.Context) error {
	if err := s.requireWorker(); err != nil {
		return err
	}

	command := taskPrologNetworkEnvCommand(s.worker.Name, taskPrologEnvImmediate, taskPrologEnvTimeout)
	output, err := s.runtime.Jail().Run(ctx, command)
	s.jobOutput = output
	s.runtime.Logf("Task prolog env: %s\n%s", command, strings.TrimSpace(output))
	if err != nil {
		return fmt.Errorf("run GPU Slurm job from login node on worker %s: %w; output:\n%s",
			s.worker.Name,
			err,
			strings.TrimSpace(output),
		)
	}
	return nil
}

func (s *TaskPrologEnv) jobEnvironmentContainsNetworkVariables() error {
	vars, err := parseTaskPrologEnvOutput(s.jobOutput)
	if err != nil {
		return err
	}
	return validateTaskPrologNetworkEnv(vars, "job environment")
}

func (s *TaskPrologEnv) runWorkerSSHLoginShell(ctx context.Context) error {
	if err := s.requireWorker(); err != nil {
		return err
	}
	if err := ensureSSHTestUser(ctx, s.runtime, sshTestUserName); err != nil {
		return err
	}
	if err := waitForSSHTestUserOnWorker(ctx, s.runtime, sshTestUserName, s.worker); err != nil {
		return err
	}

	command := taskPrologWorkerSSHNetworkEnvCommand(
		sshTestUserName,
		s.worker.Name,
		taskPrologSSHTimeout,
	)
	output, err := s.runtime.Jail().RunWithDefaultRetry(ctx, command)
	s.sshOutput = output
	s.runtime.Logf("Task prolog env: %s\n%s", command, strings.TrimSpace(output))
	if err != nil {
		return fmt.Errorf("collect NCCL network environment from worker %s SSH login shell: %w; output:\n%s",
			s.worker.Name,
			err,
			strings.TrimSpace(output),
		)
	}
	return nil
}

func (s *TaskPrologEnv) workerSSHSessionContainsNetworkVariables() error {
	vars, err := parseTaskPrologEnvOutput(s.sshOutput)
	if err != nil {
		return err
	}
	return validateTaskPrologNetworkEnv(vars, "worker SSH login environment")
}

func (s *TaskPrologEnv) requireWorker() error {
	if s.worker.Name == "" {
		return fmt.Errorf("select GPU worker for task-prolog environment checks")
	}
	return nil
}

func taskPrologNetworkEnvCommand(worker string, immediate, timeout time.Duration) string {
	args := []string{
		fmt.Sprintf("--immediate=%d", int(immediate.Seconds())),
		fmt.Sprintf("-w %s", framework.ShellQuote(worker)),
		"--nodes=1",
		"--ntasks=1",
		"--gpus-per-node=1",
		bashC(taskPrologNetworkEnvProbe()),
	}
	return fmt.Sprintf("timeout %d srun %s", int(timeout.Seconds()), strings.Join(args, " "))
}

func taskPrologWorkerSSHNetworkEnvCommand(user, worker string, timeout time.Duration) string {
	sshCommand := fmt.Sprintf(
		"timeout %d ssh -o BatchMode=yes -o LogLevel=ERROR -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null %s %s",
		int(timeout.Seconds()),
		framework.ShellQuote(worker),
		framework.ShellQuote(framework.BashLC(taskPrologNetworkEnvProbe())),
	)
	return fmt.Sprintf("su - %s -c %s",
		framework.ShellQuote(user),
		framework.ShellQuote(sshCommand),
	)
}

func bashC(script string) string {
	return "bash -c " + framework.ShellQuote(script)
}

func taskPrologNetworkEnvProbe() string {
	return strings.Join([]string{
		fmt.Sprintf("actual_ucx=\"${%s:-}\"", ucxNetDevicesName),
		fmt.Sprintf("actual_nccl=\"${%s:-}\"", ncclIBHCAName),
		fmt.Sprintf("unset %s %s", ucxNetDevicesName, ncclIBHCAName),
		fmt.Sprintf("source %s >/dev/null 2>&1 || true", framework.ShellQuote(taskPrologNCCLNetworkVarsPath)),
		fmt.Sprintf("printf '%s=%%s\\n' \"$actual_ucx\"", actualUCXNetDevicesName),
		fmt.Sprintf("printf '%s=%%s\\n' \"$actual_nccl\"", actualNCCLIBHCAName),
		fmt.Sprintf("printf '%s=%%s\\n' \"${%s:-}\"", expectedUCXNetDevicesName, ucxNetDevicesName),
		fmt.Sprintf("printf '%s=%%s\\n' \"${%s:-}\"", expectedNCCLIBHCAName, ncclIBHCAName),
	}, "; ")
}

func parseTaskPrologEnvOutput(output string) (map[string]string, error) {
	wanted := map[string]struct{}{
		actualUCXNetDevicesName:   {},
		actualNCCLIBHCAName:       {},
		expectedUCXNetDevicesName: {},
		expectedNCCLIBHCAName:     {},
	}
	vars := make(map[string]string, len(wanted))
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		name, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if _, ok := wanted[name]; !ok {
			continue
		}
		if _, duplicate := vars[name]; duplicate {
			return nil, fmt.Errorf("parse duplicate %s in task prolog env output", name)
		}
		vars[name] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan task prolog env output: %w", err)
	}
	return vars, nil
}

func validateTaskPrologNetworkEnv(vars map[string]string, source string) error {
	var problems []string
	for _, pair := range []struct {
		actual   string
		expected string
		label    string
	}{
		{actual: actualUCXNetDevicesName, expected: expectedUCXNetDevicesName, label: ucxNetDevicesName},
		{actual: actualNCCLIBHCAName, expected: expectedNCCLIBHCAName, label: ncclIBHCAName},
	} {
		actual, actualFound := vars[pair.actual]
		expected, expectedFound := vars[pair.expected]
		if !actualFound {
			problems = append(problems, fmt.Sprintf("%s is missing from %s output", pair.label, source))
			continue
		}
		if !expectedFound {
			problems = append(problems, fmt.Sprintf("%s is missing from %s output", pair.label, taskPrologNCCLNetworkVarsPath))
			continue
		}
		if expected == "" {
			problems = append(problems, fmt.Sprintf("%s is empty in %s", pair.label, taskPrologNCCLNetworkVarsPath))
			continue
		}
		if actual == "" {
			problems = append(problems, fmt.Sprintf("%s is empty in the %s", pair.label, source))
			continue
		}
		if actual != expected {
			problems = append(problems, fmt.Sprintf("%s=%q in %s, want %q from %s",
				pair.label,
				actual,
				source,
				expected,
				taskPrologNCCLNetworkVarsPath,
			))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("validate task prolog network environment: %s", strings.Join(problems, "; "))
	}
	return nil
}
