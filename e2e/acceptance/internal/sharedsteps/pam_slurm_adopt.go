package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	pamSlurmAdoptUser       = "soperatoradopt"
	pamSlurmAdoptKeyName    = "soperator_e2e_pam_slurm_adopt"
	pamSlurmAdoptKeyComment = "soperator-e2e-pam-slurm-adopt"
	pamSlurmAdoptStatusPath = "/home/soperatoradopt/.soperator-e2e-pam-slurm-adopt-status"

	pamSlurmAdoptRolloutTimeout    = 15 * time.Minute
	pamSlurmAdoptJobStartTimeout   = 5 * time.Minute
	pamSlurmAdoptSSHTimeout        = 10 * time.Minute
	pamSlurmAdoptSessionTimeout    = 2 * time.Minute
	pamSlurmAdoptCleanupTimeout    = 15 * time.Minute
	pamSlurmAdoptStatusWaitTimeout = 2 * time.Minute
)

var enabledPAMSlurmAdoptConfig = json.RawMessage(`{
	"enabled": true,
	"actionUnknown": "newest",
	"exemptUsers": [],
	"exemptGroups": []
}`)

type pamSlurmAdoptSSHResult struct {
	output string
	err    error
}

type pamSlurmAdoptStatus struct {
	cgroup   string
	gpuCount int
}

type PAMSlurmAdopt struct {
	info     *framework.ClusterInfo
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector

	originalConfig json.RawMessage
	configRecorded bool
	restoreNeeded  bool
	identitySet    bool
	worker         framework.WorkerInfo
	job            framework.SbatchJob
	status         pamSlurmAdoptStatus
	sshCancel      context.CancelFunc
	sshResults     chan pamSlurmAdoptSSHResult
}

func NewPAMSlurmAdopt(
	info *framework.ClusterInfo,
	runtime framework.Runtime,
	slurm *framework.SlurmClient,
	kubectl *framework.KubectlClient,
	selector *framework.WorkerSelector,
) *PAMSlurmAdopt {
	return &PAMSlurmAdopt{
		info:     info,
		runtime:  runtime,
		slurm:    slurm,
		kubectl:  kubectl,
		selector: selector,
	}
}

func (s *PAMSlurmAdopt) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^PAM Slurm adopt is enabled for worker SSH$`, s.pamSlurmAdoptIsEnabledForWorkerSSH)
	sc.Step(`^a PAM Slurm adopt test user and GPU worker are ready$`, s.aPAMSlurmAdoptTestUserAndGPUWorkerAreReady)
	sc.Step(`^SSH to the worker without a job is denied$`, s.sshToTheWorkerWithoutAJobIsDenied)
	sc.Step(`^a long-running GPU job owned by the user starts on the worker$`, s.aLongRunningGPUJobOwnedByTheUserStartsOnTheWorker)
	sc.Step(`^the user opens a long-running SSH session to the worker$`, s.theUserOpensALongRunningSSHSessionToTheWorker)
	sc.Step(`^the SSH session is in the job extern cgroup$`, s.theSSHSessionIsInTheJobExternCgroup)
	sc.Step(`^the SSH session sees only the job's allocated GPU$`, s.theSSHSessionSeesOnlyTheJobsAllocatedGPU)
	sc.Step(`^the PAM Slurm adopt job is cancelled$`, s.thePAMSlurmAdoptJobIsCancelled)
	sc.Step(`^the adopted SSH session terminates$`, s.theAdoptedSSHSessionTerminates)
}

func (s *PAMSlurmAdopt) CleanupAndReset(ctx context.Context) {
	cleanupCtx, cancel := context.WithTimeout(ctx, pamSlurmAdoptCleanupTimeout)
	defer cancel()

	s.stopSSHSession(cleanupCtx)
	if !s.job.IsZero() {
		if err := s.slurm.CancelJob(cleanupCtx, s.job.ID, pamSlurmAdoptSessionTimeout); err != nil {
			s.runtime.Logf("cleanup: cancel PAM Slurm adopt job: %v", err)
		}
	}
	if s.identitySet {
		if _, err := s.runtime.Jail().Run(cleanupCtx, "rm -f -- "+framework.ShellQuote(pamSlurmAdoptStatusPath)); err != nil {
			s.runtime.Logf("cleanup: remove PAM Slurm adopt status file: %v", err)
		}
		if err := removeSSHTestIdentity(
			cleanupCtx,
			s.runtime,
			pamSlurmAdoptUser,
			pamSlurmAdoptKeyName,
			pamSlurmAdoptKeyComment,
		); err != nil {
			s.runtime.Logf("cleanup: remove PAM Slurm adopt SSH identity: %v", err)
		}
	}
	if s.restoreNeeded && s.configRecorded {
		if err := s.kubectl.PatchSlurmClusterPAMSlurmAdopt(
			cleanupCtx,
			s.info.SlurmClusterName,
			s.originalConfig,
		); err != nil {
			s.runtime.Logf("cleanup: restore PAM Slurm adopt configuration: %v", err)
		} else if enabled, err := pamSlurmAdoptEnabled(s.originalConfig); err != nil {
			s.runtime.Logf("cleanup: read original PAM Slurm adopt configuration: %v", err)
		} else if err := s.waitForWorkerPAMSlurmAdoptState(cleanupCtx, enabled); err != nil {
			s.runtime.Logf("cleanup: wait for original PAM Slurm adopt worker state: %v", err)
		}
	}

	s.originalConfig = nil
	s.configRecorded = false
	s.restoreNeeded = false
	s.identitySet = false
	s.worker = framework.WorkerInfo{}
	s.job = framework.SbatchJob{}
	s.status = pamSlurmAdoptStatus{}
	s.sshCancel = nil
	s.sshResults = nil
}

func (s *PAMSlurmAdopt) pamSlurmAdoptIsEnabledForWorkerSSH(ctx context.Context) error {
	cluster, err := s.kubectl.SlurmCluster(ctx, s.info.SlurmClusterName)
	if err != nil {
		return err
	}
	s.originalConfig = append(json.RawMessage(nil), cluster.PAMSlurmAdopt...)
	s.configRecorded = true
	alreadyEnabled, err := pamSlurmAdoptEnabled(s.originalConfig)
	if err != nil {
		return err
	}

	if !alreadyEnabled {
		s.restoreNeeded = true
		if err := s.kubectl.PatchSlurmClusterPAMSlurmAdopt(
			ctx,
			s.info.SlurmClusterName,
			enabledPAMSlurmAdoptConfig,
		); err != nil {
			return err
		}
	}
	if err := s.waitForPAMSlurmAdoptSlurmConfig(ctx); err != nil {
		return err
	}
	return s.waitForWorkerPAMSlurmAdoptState(ctx, true)
}

func (s *PAMSlurmAdopt) aPAMSlurmAdoptTestUserAndGPUWorkerAreReady(ctx context.Context) error {
	workers, err := s.selector.PickGPUWorkers(ctx, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]

	if err := ensureSSHTestUser(ctx, s.runtime, pamSlurmAdoptUser); err != nil {
		return err
	}
	s.identitySet = true
	if err := ensureSSHTestIdentity(
		ctx,
		s.runtime,
		pamSlurmAdoptUser,
		pamSlurmAdoptKeyName,
		pamSlurmAdoptKeyComment,
	); err != nil {
		return err
	}
	if err := waitForSSHTestUserOnWorker(ctx, s.runtime, pamSlurmAdoptUser, s.worker); err != nil {
		return err
	}
	if _, err := runSSHCommand(
		ctx,
		s.runtime,
		pamSlurmAdoptUser,
		pamSlurmAdoptKeyName,
		"localhost",
		30*time.Second,
		"true",
	); err != nil {
		return fmt.Errorf("verify PAM Slurm adopt test identity on login node: %w", err)
	}
	if _, err := s.runtime.Worker(s.worker).RunWithDefaultRetry(ctx, "true"); err != nil {
		return fmt.Errorf("verify root SSH transport to worker %s: %w", s.worker.Name, err)
	}

	command := fmt.Sprintf(
		"scancel -u %s >/dev/null 2>&1 || true; rm -f -- %s",
		framework.ShellQuote(pamSlurmAdoptUser),
		framework.ShellQuote(pamSlurmAdoptStatusPath),
	)
	if _, err := s.runtime.Jail().Run(ctx, command); err != nil {
		return fmt.Errorf("reset PAM Slurm adopt test state: %w", err)
	}
	return s.waitForPAMSlurmAdoptUserJobsGone(ctx)
}

func (s *PAMSlurmAdopt) sshToTheWorkerWithoutAJobIsDenied(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("test PAM Slurm adopt denial: worker is not selected")
	}
	output, err := runSSHCommand(
		ctx,
		s.runtime,
		pamSlurmAdoptUser,
		pamSlurmAdoptKeyName,
		s.worker.Name,
		30*time.Second,
		"hostname",
	)
	if err == nil {
		return fmt.Errorf("expected SSH without a job to be denied, got output %q", strings.TrimSpace(output))
	}
	if strings.Contains(err.Error(), "exit status 124") {
		return fmt.Errorf("SSH without a job timed out instead of being denied: %w", err)
	}
	return nil
}

func (s *PAMSlurmAdopt) aLongRunningGPUJobOwnedByTheUserStartsOnTheWorker(ctx context.Context) error {
	if s.worker.Name == "" {
		return fmt.Errorf("submit PAM Slurm adopt job: worker is not selected")
	}
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:     "e2e-pam-slurm-adopt",
		Nodes:       1,
		Nodelist:    []string{s.worker.Name},
		GPUsPerNode: 1,
		Wrap:        "sleep 3600",
		RunAsUser:   pamSlurmAdoptUser,
	})
	if err != nil {
		return err
	}
	s.job = job
	return s.slurm.WaitForJobRunning(ctx, job.ID, pamSlurmAdoptJobStartTimeout)
}

func (s *PAMSlurmAdopt) theUserOpensALongRunningSSHSessionToTheWorker(ctx context.Context) error {
	if s.job.IsZero() {
		return fmt.Errorf("open adopted SSH session: job is not running")
	}
	if s.sshCancel != nil {
		return fmt.Errorf("open adopted SSH session: a session is already active")
	}

	remoteCommand := fmt.Sprintf(`
set -euo pipefail
status=%s
tmp="${status}.tmp.$$"
gpu_count="$(nvidia-smi --query-gpu=index --format=csv,noheader | sed '/^[[:space:]]*$/d' | wc -l | tr -d '[:space:]')"
{
    printf 'cgroup=%%s\n' "$(cat /proc/self/cgroup)"
    printf 'gpu_count=%%s\n' "${gpu_count}"
} >"${tmp}"
mv -f -- "${tmp}" "${status}"
while true; do sleep 30; done
`, framework.ShellQuote(pamSlurmAdoptStatusPath))

	sshCtx, cancel := context.WithCancel(context.Background())
	s.sshCancel = cancel
	results := make(chan pamSlurmAdoptSSHResult, 1)
	s.sshResults = results
	host := s.worker.Name
	go func(results chan<- pamSlurmAdoptSSHResult, host string) {
		output, err := runSSHCommand(
			sshCtx,
			s.runtime,
			pamSlurmAdoptUser,
			pamSlurmAdoptKeyName,
			host,
			pamSlurmAdoptSSHTimeout,
			remoteCommand,
		)
		results <- pamSlurmAdoptSSHResult{output: output, err: err}
	}(results, host)

	var statusOutput string
	err := s.runtime.WaitFor(ctx,
		"PAM Slurm adopt SSH session status",
		pamSlurmAdoptStatusWaitTimeout,
		framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			select {
			case result := <-s.sshResults:
				s.sshResults = nil
				return false, fmt.Errorf("SSH session ended before publishing status: %v; output: %s", result.err, strings.TrimSpace(result.output))
			default:
			}

			command := fmt.Sprintf(
				"su - %s -c %s",
				framework.ShellQuote(pamSlurmAdoptUser),
				framework.ShellQuote("test -s "+framework.ShellQuote(pamSlurmAdoptStatusPath)+" && cat "+framework.ShellQuote(pamSlurmAdoptStatusPath)),
			)
			output, err := s.runtime.Jail().Run(waitCtx, command)
			if err != nil {
				return false, err
			}
			statusOutput = output
			return true, nil
		},
	)
	if err != nil {
		return err
	}

	s.status, err = parsePAMSlurmAdoptStatus(statusOutput)
	return err
}

func (s *PAMSlurmAdopt) theSSHSessionIsInTheJobExternCgroup() error {
	if !isSlurmExternCgroup(s.status.cgroup) {
		return fmt.Errorf("SSH session cgroup %q does not contain a Slurm extern step", s.status.cgroup)
	}
	return nil
}

func (s *PAMSlurmAdopt) theSSHSessionSeesOnlyTheJobsAllocatedGPU() error {
	if s.status.gpuCount != 1 {
		return fmt.Errorf("adopted SSH session sees %d GPUs, expected 1", s.status.gpuCount)
	}
	return nil
}

func (s *PAMSlurmAdopt) thePAMSlurmAdoptJobIsCancelled(ctx context.Context) error {
	if s.job.IsZero() {
		return fmt.Errorf("cancel PAM Slurm adopt job: job ID is empty")
	}
	if s.sshResults == nil {
		return fmt.Errorf("cancel PAM Slurm adopt job: SSH session is not active")
	}
	select {
	case result := <-s.sshResults:
		s.sshResults = nil
		return fmt.Errorf("adopted SSH session ended before job cancellation: %v; output: %s", result.err, strings.TrimSpace(result.output))
	default:
	}
	return s.slurm.CancelJob(ctx, s.job.ID, pamSlurmAdoptSessionTimeout)
}

func (s *PAMSlurmAdopt) theAdoptedSSHSessionTerminates(ctx context.Context) error {
	if s.sshResults == nil {
		return fmt.Errorf("wait for adopted SSH session: session is not active")
	}
	timer := time.NewTimer(pamSlurmAdoptSessionTimeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("adopted SSH session remained active for %s after job cancellation", pamSlurmAdoptSessionTimeout)
	case result := <-s.sshResults:
		s.sshResults = nil
		s.sshCancel()
		s.sshCancel = nil
		s.job = framework.SbatchJob{}
		s.runtime.Logf("PAM Slurm adopt SSH session terminated after job cancellation: %v", result.err)
		return nil
	}
}

func (s *PAMSlurmAdopt) waitForPAMSlurmAdoptSlurmConfig(ctx context.Context) error {
	return s.runtime.WaitFor(ctx,
		"Slurm PAM adoption settings",
		pamSlurmAdoptRolloutTimeout,
		framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			configuration, err := s.slurm.Configuration(waitCtx)
			if err != nil {
				return false, err
			}
			if !slurmSettingContains(configuration["PrologFlags"], "contain") {
				return false, fmt.Errorf("observe PrologFlags=%q without contain", configuration["PrologFlags"])
			}
			if !slurmSettingContains(configuration["LaunchParameters"], "ulimit_pam_adopt") {
				return false, fmt.Errorf("observe LaunchParameters=%q without ulimit_pam_adopt", configuration["LaunchParameters"])
			}
			return true, nil
		},
	)
}

func (s *PAMSlurmAdopt) waitForWorkerPAMSlurmAdoptState(ctx context.Context, enabled bool) error {
	return s.runtime.WaitFor(ctx,
		fmt.Sprintf("all workers to report PAM Slurm adopt enabled=%t", enabled),
		pamSlurmAdoptRolloutTimeout,
		framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			nodeSets, err := s.kubectl.NodeSets(waitCtx, s.info.SlurmClusterName)
			if err != nil {
				return false, err
			}
			if len(nodeSets) == 0 {
				return false, fmt.Errorf("observe no NodeSets for Slurm cluster %s", s.info.SlurmClusterName)
			}
			desiredWorkers := 0
			for _, nodeSet := range nodeSets {
				desiredWorkers += nodeSet.Replicas
				if nodeSet.ReadyReplicas != nodeSet.Replicas {
					return false, fmt.Errorf(
						"observe NodeSet %s with %d/%d ready replicas",
						nodeSet.Name,
						nodeSet.ReadyReplicas,
						nodeSet.Replicas,
					)
				}
			}

			pods, err := s.kubectl.WorkerPods(waitCtx)
			if err != nil {
				return false, err
			}
			if len(pods) != desiredWorkers {
				return false, fmt.Errorf("observe %d worker pods, expected %d", len(pods), desiredWorkers)
			}
			for _, pod := range pods {
				if !pod.Ready {
					return false, fmt.Errorf("observe worker pod %s not ready", pod.PodName)
				}
				value, found := pod.SlurmdEnvironment["SOPERATOR_PAM_SLURM_ADOPT_ENABLED"]
				if enabled && (!found || value != "true") {
					return false, fmt.Errorf("observe PAM Slurm adopt disabled in worker pod %s", pod.PodName)
				}
				if !enabled && found {
					return false, fmt.Errorf("observe PAM Slurm adopt environment in worker pod %s", pod.PodName)
				}
			}
			return true, nil
		},
	)
}

func (s *PAMSlurmAdopt) waitForPAMSlurmAdoptUserJobsGone(ctx context.Context) error {
	return s.runtime.WaitFor(ctx,
		"PAM Slurm adopt test user's jobs to leave the queue",
		pamSlurmAdoptSessionTimeout,
		framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			output, err := s.runtime.Jail().RunWithDefaultRetry(waitCtx,
				"squeue -h -u "+framework.ShellQuote(pamSlurmAdoptUser))
			if err != nil {
				return false, err
			}
			return strings.TrimSpace(output) == "", nil
		},
	)
}

func (s *PAMSlurmAdopt) stopSSHSession(ctx context.Context) {
	if s.sshCancel == nil {
		return
	}
	s.sshCancel()
	s.sshCancel = nil
	if s.sshResults == nil {
		return
	}
	select {
	case <-s.sshResults:
	case <-ctx.Done():
	}
	s.sshResults = nil
}

func pamSlurmAdoptEnabled(config json.RawMessage) (bool, error) {
	if len(config) == 0 || string(config) == "null" {
		return false, nil
	}
	var value struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(config, &value); err != nil {
		return false, fmt.Errorf("decode PAM Slurm adopt configuration: %w", err)
	}
	return value.Enabled, nil
}

func slurmSettingContains(value, expected string) bool {
	for field := range strings.SplitSeq(value, ",") {
		if strings.EqualFold(strings.TrimSpace(field), expected) {
			return true
		}
	}
	return false
}

func parsePAMSlurmAdoptStatus(output string) (pamSlurmAdoptStatus, error) {
	values := make(map[string]string)
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) == "" {
			return pamSlurmAdoptStatus{}, fmt.Errorf("parse PAM Slurm adopt status line %q", line)
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	cgroup := values["cgroup"]
	if cgroup == "" {
		return pamSlurmAdoptStatus{}, errors.New("parse PAM Slurm adopt status: cgroup is empty")
	}
	gpuCount, err := strconv.Atoi(values["gpu_count"])
	if err != nil || gpuCount < 0 {
		return pamSlurmAdoptStatus{}, fmt.Errorf("parse PAM Slurm adopt GPU count %q", values["gpu_count"])
	}
	return pamSlurmAdoptStatus{cgroup: cgroup, gpuCount: gpuCount}, nil
}

func isSlurmExternCgroup(value string) bool {
	for line := range strings.SplitSeq(value, "\n") {
		_, path, found := strings.Cut(line, "::")
		if !found {
			fields := strings.SplitN(line, ":", 3)
			if len(fields) == 3 {
				path = fields[2]
				found = true
			}
		}
		if !found {
			continue
		}
		for component := range strings.SplitSeq(strings.Trim(path, "/"), "/") {
			if component == "step_extern" {
				return true
			}
		}
	}
	return false
}
