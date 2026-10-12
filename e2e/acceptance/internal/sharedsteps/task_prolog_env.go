package sharedsteps

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	taskPrologEnvTimeout   = 5 * time.Minute
	taskPrologEnvImmediate = 120 * time.Second

	ucxNetDevicesName = "UCX_NET_DEVICES"
	ncclIBHCAName     = "NCCL_IB_HCA"
)

var taskPrologNetworkValuePattern = regexp.MustCompile(`^mlx5_[0-9]+:[0-9]+(,mlx5_[0-9]+:[0-9]+)*$`)

type TaskPrologEnv struct {
	runtime  framework.Runtime
	selector *framework.WorkerSelector

	worker framework.WorkerInfo
	output string
}

func NewTaskPrologEnv(runtime framework.Runtime, selector *framework.WorkerSelector) *TaskPrologEnv {
	return &TaskPrologEnv{
		runtime:  runtime,
		selector: selector,
	}
}

func (s *TaskPrologEnv) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a healthy GPU worker is selected for task-prolog environment checks$`, s.selectHealthyGPUWorker)
	sc.Step(`^a GPU Slurm job launched from the login node prints its network environment$`, s.runGPUJobFromLogin)
	sc.Step(`^the job environment contains NCCL network variables$`, s.jobEnvironmentContainsNetworkVariables)
}

func (s *TaskPrologEnv) CleanupAndReset(ctx context.Context) {
	s.worker = framework.WorkerInfo{}
	s.output = ""
}

func (s *TaskPrologEnv) selectHealthyGPUWorker(ctx context.Context) error {
	workers, err := s.selector.PickGPUWorkers(ctx, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]
	s.runtime.Logf("task prolog env: selected GPU worker=%s", s.worker.Name)
	return nil
}

func (s *TaskPrologEnv) runGPUJobFromLogin(ctx context.Context) error {
	if err := s.requireWorker(); err != nil {
		return err
	}

	command := taskPrologNetworkEnvCommand(s.worker.Name, taskPrologEnvImmediate, taskPrologEnvTimeout)
	output, err := s.runtime.Jail().Run(ctx, command)
	s.output = output
	s.runtime.Logf("task prolog env: %s\n%s", command, strings.TrimSpace(output))
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
	vars, err := parseTaskPrologEnvOutput(s.output)
	if err != nil {
		return err
	}
	return validateTaskPrologNetworkEnv(vars)
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
		framework.BashLC(taskPrologNetworkEnvProbe()),
	}
	return fmt.Sprintf("timeout %d srun %s", int(timeout.Seconds()), strings.Join(args, " "))
}

func taskPrologNetworkEnvProbe() string {
	return strings.Join([]string{
		fmt.Sprintf("printf '%s=%%s\\n' \"${%s:-}\"", ucxNetDevicesName, ucxNetDevicesName),
		fmt.Sprintf("printf '%s=%%s\\n' \"${%s:-}\"", ncclIBHCAName, ncclIBHCAName),
	}, "; ")
}

func parseTaskPrologEnvOutput(output string) (map[string]string, error) {
	wanted := map[string]struct{}{
		ucxNetDevicesName: {},
		ncclIBHCAName:     {},
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

func validateTaskPrologNetworkEnv(vars map[string]string) error {
	var problems []string
	for _, name := range []string{ucxNetDevicesName, ncclIBHCAName} {
		value, found := vars[name]
		if !found {
			problems = append(problems, fmt.Sprintf("%s is missing", name))
			continue
		}
		if value == "" {
			problems = append(problems, fmt.Sprintf("%s is empty", name))
			continue
		}
		if !taskPrologNetworkValuePattern.MatchString(value) {
			problems = append(problems, fmt.Sprintf("%s=%q does not match mlx5 device list format", name, value))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("validate task prolog network environment: %s", strings.Join(problems, "; "))
	}
	return nil
}
