package sharedsteps

import (
	"context"
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

	pamSlurmAdoptJobTimeout     = 5 * time.Minute
	pamSlurmAdoptSessionTimeout = 2 * time.Minute
	pamSlurmAdoptCleanupTimeout = 15 * time.Minute
)

type pamSlurmAdoptSSHResult struct {
	output string
	err    error
}

type PAMSlurmAdopt struct {
	info     *framework.ClusterInfo
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector

	worker      framework.WorkerInfo
	job         framework.SbatchJob
	jobDevices  string
	identitySet bool
	sshCancel   context.CancelFunc
	sshResults  chan pamSlurmAdoptSSHResult
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
	sc.Step("^PAM Slurm adopt is enabled$", s.pamSlurmAdoptIsEnabled)
	sc.Step("^a PAM Slurm adopt test user and GPU worker are ready$", s.aTestUserAndGPUWorkerAreReady)
	sc.Step("^SSH to the worker without a job is denied$", s.sshWithoutAJobIsDenied)
	sc.Step("^the user starts a GPU job and opens SSH to its worker$", s.startGPUJobAndSSH)
	sc.Step("^the SSH session is adopted and ends with the job$", s.sshSessionIsAdoptedAndEndsWithJob)
}

func (s *PAMSlurmAdopt) pamSlurmAdoptIsEnabled(ctx context.Context) error {
	cluster, err := s.kubectl.SlurmCluster(ctx, s.info.SlurmClusterName)
	if err != nil {
		return err
	}
	if !cluster.PAMSlurmAdoptEnabled {
		s.runtime.Logf("PAM Slurm adopt is disabled, skipping scenario")
		return godog.ErrSkip
	}

	return nil
}

func (s *PAMSlurmAdopt) CleanupAndReset(ctx context.Context) {
	cleanupCtx, cancel := context.WithTimeout(ctx, pamSlurmAdoptCleanupTimeout)
	defer cancel()

	s.stopSSH(cleanupCtx)
	if !s.job.IsZero() {
		if err := s.slurm.CancelJob(cleanupCtx, s.job.ID, pamSlurmAdoptSessionTimeout); err != nil {
			s.runtime.Logf("cleanup: cancel PAM Slurm adopt job: %v", err)
		}
	}
	if s.identitySet {
		_, _ = s.runtime.Jail().Run(cleanupCtx, "rm -f -- "+framework.ShellQuote(pamSlurmAdoptStatusPath))
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

	s.worker = framework.WorkerInfo{}
	s.job = framework.SbatchJob{}
	s.jobDevices = ""
	s.identitySet = false
	s.sshCancel = nil
	s.sshResults = nil
}

func (s *PAMSlurmAdopt) aTestUserAndGPUWorkerAreReady(ctx context.Context) error {
	workers, err := s.selector.PickGPUWorkers(ctx, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]

	workerPod, err := s.kubectl.WorkerPodForSlurmNode(ctx, s.worker.Name)
	if err != nil {
		return err
	}
	if _, err := s.runtime.WorkerPod(workerPod).RunWithDefaultRetry(
		ctx,
		"grep -Eq '^[[:space:]]*-?account[[:space:]]+required[[:space:]]+pam_slurm_adopt\\.so' /etc/pam.d/soperator-pam-slurm-adopt",
	); err != nil {
		return fmt.Errorf("verify pam_slurm_adopt worker policy: %w", err)
	}
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
	return s.waitForUserJobsGone(ctx)
}

func (s *PAMSlurmAdopt) sshWithoutAJobIsDenied(ctx context.Context) error {
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
	if sshCommandTimedOut(err) {
		return fmt.Errorf("SSH without a job timed out instead of being denied: %w", err)
	}
	return nil
}

func (s *PAMSlurmAdopt) startGPUJobAndSSH(ctx context.Context) error {
	probe := `import os, pathlib, time
for directory in ("/mnt/memory", "/dev/shm", "/tmp"):
    (pathlib.Path(directory) / "soperator-pam-tmpfs").write_text(os.environ["SLURM_JOB_ID"])
print("PAM_JOB_DEVICES=" + ",".join(str(pathlib.Path(p).stat().st_dev) for p in ("/mnt/memory", "/dev/shm", "/tmp")), flush=True)
time.sleep(3600)
`
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:     "e2e-pam-slurm-adopt",
		Nodes:       1,
		Nodelist:    []string{s.worker.Name},
		GPUsPerNode: 1,
		Wrap:        "python3 -u -c " + framework.ShellQuote(probe),
		RunAsUser:   pamSlurmAdoptUser,
	})
	if err != nil {
		return err
	}
	s.job = job
	if err := s.slurm.WaitForJobRunning(ctx, job.ID, pamSlurmAdoptJobTimeout); err != nil {
		return err
	}
	if err := framework.WaitForWithJobAlive(ctx, s.runtime, s.slurm, job, "PAM job tmpfs markers", pamSlurmAdoptSessionTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			output, err := readJobFile(waitCtx, s.runtime, job.StdoutPath)
			if err != nil {
				return false, err
			}
			s.jobDevices = parseKeyValueLine(output, "PAM_JOB_DEVICES")
			return s.jobDevices != "", nil
		}); err != nil {
		return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, job, err)
	}

	remoteCommand := fmt.Sprintf(
		"set -euo pipefail; status=%s; "+
			"for directory in /mnt/memory /dev/shm /tmp; do test \"$(cat \"$directory/soperator-pam-tmpfs\")\" = %s; done; "+
			"gpu_count=$(nvidia-smi --query-gpu=index --format=csv,noheader | sed '/^[[:space:]]*$/d' | wc -l | tr -d '[:space:]'); "+
			"{ printf 'cgroup=%%s\\n' \"$(cat /proc/self/cgroup)\"; printf 'gpu_count=%%s\\n' \"$gpu_count\"; "+
			"printf 'tmpfs_devices=%%s\\n' \"$(stat -c '%%d' /mnt/memory /dev/shm /tmp | paste -sd,)\"; } >\"$status\"; "+
			"exec sleep 3600",
		framework.ShellQuote(pamSlurmAdoptStatusPath),
		framework.ShellQuote(job.ID),
	)
	sshCtx, cancel := context.WithCancel(context.Background())
	s.sshCancel = cancel
	s.sshResults = make(chan pamSlurmAdoptSSHResult, 1)
	go func() {
		output, sshErr := runSSHCommand(
			sshCtx,
			s.runtime,
			pamSlurmAdoptUser,
			pamSlurmAdoptKeyName,
			s.worker.Name,
			10*time.Minute,
			remoteCommand,
		)
		s.sshResults <- pamSlurmAdoptSSHResult{output: output, err: sshErr}
	}()

	return s.runtime.WaitFor(
		ctx,
		"PAM Slurm adopt SSH status",
		pamSlurmAdoptSessionTimeout,
		framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			select {
			case result := <-s.sshResults:
				s.sshResults = nil
				return false, fmt.Errorf("SSH session ended before adoption was verified: %v; output: %s", result.err, strings.TrimSpace(result.output))
			default:
			}
			output, readErr := s.runtime.Jail().Run(
				waitCtx,
				"test -s "+framework.ShellQuote(pamSlurmAdoptStatusPath)+" && cat "+framework.ShellQuote(pamSlurmAdoptStatusPath),
			)
			if readErr != nil {
				return false, readErr
			}
			return validatePAMSlurmAdoptStatus(output, s.jobDevices)
		},
	)
}

func (s *PAMSlurmAdopt) sshSessionIsAdoptedAndEndsWithJob(ctx context.Context) error {
	if err := s.slurm.CancelJob(ctx, s.job.ID, pamSlurmAdoptSessionTimeout); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(pamSlurmAdoptSessionTimeout):
		return fmt.Errorf("adopted SSH session remained active after job cancellation")
	case result := <-s.sshResults:
		s.sshResults = nil
		s.sshCancel()
		s.sshCancel = nil
		s.job = framework.SbatchJob{}
		if result.err == nil {
			return fmt.Errorf("adopted SSH command exited successfully after job cancellation")
		}
		if err := s.waitForUserProcessesGone(ctx); err != nil {
			return err
		}
		return s.sshWithoutAJobIsDenied(ctx)
	}
}

func (s *PAMSlurmAdopt) waitForUserJobsGone(ctx context.Context) error {
	return s.runtime.WaitFor(
		ctx,
		"PAM Slurm adopt test user's jobs to leave the queue",
		pamSlurmAdoptSessionTimeout,
		framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			output, err := s.runtime.Jail().RunWithDefaultRetry(
				waitCtx,
				"squeue -h -u "+framework.ShellQuote(pamSlurmAdoptUser),
			)
			if err != nil {
				return false, err
			}
			return strings.TrimSpace(output) == "", nil
		},
	)
}

func (s *PAMSlurmAdopt) waitForUserProcessesGone(ctx context.Context) error {
	command := fmt.Sprintf(
		"if pgrep -u %s -a; then exit 1; fi",
		framework.ShellQuote(pamSlurmAdoptUser),
	)
	return s.runtime.WaitFor(
		ctx,
		fmt.Sprintf("PAM Slurm adopt user processes to leave worker %s", s.worker.Name),
		pamSlurmAdoptSessionTimeout,
		framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			if _, err := s.runtime.Worker(s.worker).Run(waitCtx, command); err != nil {
				return false, err
			}
			return true, nil
		},
	)
}

func (s *PAMSlurmAdopt) stopSSH(ctx context.Context) {
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

func validatePAMSlurmAdoptStatus(output, expectedDevices string) (bool, error) {
	values := make(map[string]string)
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if !isSlurmExternCgroup(values["cgroup"]) {
		return false, fmt.Errorf("SSH session cgroup %q does not contain step_extern", values["cgroup"])
	}
	if expectedDevices == "" || values["tmpfs_devices"] != expectedDevices {
		return false, fmt.Errorf("SSH tmpfs devices %q differ from job devices %q", values["tmpfs_devices"], expectedDevices)
	}
	gpuCount, err := strconv.Atoi(values["gpu_count"])
	if err != nil {
		return false, fmt.Errorf("parse PAM Slurm adopt GPU count %q", values["gpu_count"])
	}
	if gpuCount != 1 {
		return false, fmt.Errorf("adopted SSH session sees %d GPUs, expected 1", gpuCount)
	}
	return true, nil
}

func isSlurmExternCgroup(value string) bool {
	for line := range strings.SplitSeq(value, "\n") {
		fields := strings.SplitN(line, ":", 3)
		if len(fields) != 3 {
			continue
		}
		for component := range strings.SplitSeq(strings.Trim(fields[2], "/"), "/") {
			if component == "step_extern" {
				return true
			}
		}
	}
	return false
}

func sshCommandTimedOut(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "exit status 124") || strings.Contains(message, "exit code 124")
}
