package sharedsteps

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	systemSettingsJobTimeout = 5 * time.Minute
	systemSettingsCleanup    = 3 * time.Minute
	systemSettingsSSHTimeout = time.Minute

	interactiveJobImmediate = 2 * time.Minute

	loginSSHKeyName    = "soperator_e2e_login_ssh"
	loginSSHKeyComment = "soperator-e2e-login-ssh"

	jobUserKey = "JOB_USER"

	limitDirectMarker = "__soperator_acceptance_direct_limits__"
	limitNestedMarker = "__soperator_acceptance_nested_limits__"

	limitRealtimeNonblocking = "realtime_nonblocking"
	limitCoreFileSize        = "core_file_size"
	limitDataSize            = "data_size"
	limitSchedulingPriority  = "scheduling_priority"
	limitFileSize            = "file_size"
	limitPendingSignals      = "pending_signals"
	limitLockedMemory        = "locked_memory"
	limitMaxMemory           = "max_memory"
	limitOpenFiles           = "open_files"
	limitPipeSize            = "pipe_size"
	limitMessageQueues       = "message_queues"
	limitRealtimePriority    = "realtime_priority"
	limitStackSize           = "stack_size"
	limitCPUTime             = "cpu_time"
	limitMaxUserProcesses    = "max_user_processes"
	limitVirtualMemory       = "virtual_memory"
	limitFileLocks           = "file_locks"

	kernelFileMax              = "fs.file-max"
	kernelMaxMapCount          = "vm.max_map_count"
	kernelUnprivilegedUserNS   = "kernel.unprivileged_userns_clone"
	kernelReceiveBufferMax     = "net.core.rmem_max"
	kernelSendBufferMax        = "net.core.wmem_max"
	kernelTCPReceiveBufferSize = "net.ipv4.tcp_rmem"
	kernelTCPSendBufferSize    = "net.ipv4.tcp_wmem"

	slurmdContainerName              = "slurmd"
	nvidiaDriverCapabilitiesName     = "NVIDIA_DRIVER_CAPABILITIES"
	nativeResourceLimitsJobName      = "e2e-system-limits-native"
	enrootResourceLimitsJobName      = "e2e-system-limits-enroot"
	dockerResourceLimitsJobName      = "e2e-system-limits-docker"
	regularUserResourceLimitsJobName = "e2e-system-limits-regular-user"
	adoptedSSHHolderJobName          = "e2e-system-adopted-ssh-holder"
	nativeKernelSettingsJobName      = "e2e-system-kernel-native"
	enrootKernelSettingsJobName      = "e2e-system-kernel-enroot"
	dockerKernelSettingsJobName      = "e2e-system-kernel-docker"
	nativeNVIDIALibrariesJobName     = "e2e-system-nvidia-native"
	enrootNVIDIALibrariesJobName     = "e2e-system-nvidia-enroot"
	dockerNVIDIALibrariesJobName     = "e2e-system-nvidia-docker"

	nvidiaLibraryKey = "NVIDIA_LIBRARY"
	// Same threshold as MIN_REAL_LIBRARY_SIZE in nvidia_libraries.py: the jail
	// keeps small placeholder files for driver libraries of unmounted capabilities.
	nvidiaNonPlaceholderLibraryMinSize = 1024

	systemSettingsDockerContainerPrefix = "e2e-system-settings-"
)

// probeContainer selects where a system-settings job step runs its probe.
type probeContainer int

const (
	probeContainerNone probeContainer = iota
	probeContainerEnroot
	probeContainerDocker
	// GPU containers ask for the required NVIDIA driver capabilities. The Enroot
	// NVIDIA hook needs an image that requests driver injection, while Docker
	// injects the driver for --gpus into any image, so it keeps the small one.
	probeContainerEnrootGPU
	probeContainerDockerGPU
)

func (c probeContainer) isDocker() bool {
	return c == probeContainerDocker || c == probeContainerDockerGPU
}

// probeJobSpec describes a single-task system-settings job on the selected worker.
type probeJobSpec struct {
	name      string
	probe     string
	container probeContainer
	// user submits the job as this user and makes the probe report the user its
	// task runs as; empty submits as root.
	user string
	gpus int
}

// nvidiaCapabilityLibraries lists, per required NVIDIA driver capability, the
// driver libraries whose presence shows that the capability was delivered to
// the job. Presence does not prove the libraries load.
var nvidiaCapabilityLibraries = []struct {
	capability string
	libraries  []string
}{
	{capability: "compute", libraries: []string{"libcuda.so.1"}},
	{capability: "graphics", libraries: []string{"libEGL_nvidia.so.0", "libGLX_nvidia.so.0"}},
	{capability: "utility", libraries: []string{"libnvidia-ml.so.1"}},
	{capability: "video", libraries: []string{"libnvcuvid.so.1", "libnvidia-encode.so.1"}},
}

type nvidiaLibraryLookup struct {
	capability string
	library    string
	path       string
	size       int64
}

type nvidiaLibraryJobResult struct {
	kind    string
	lookups []nvidiaLibraryLookup
}

var resourceLimitOptions = []struct {
	name   string
	option string
}{
	{name: limitRealtimeNonblocking, option: "R"},
	{name: limitCoreFileSize, option: "c"},
	{name: limitDataSize, option: "d"},
	{name: limitSchedulingPriority, option: "e"},
	{name: limitFileSize, option: "f"},
	{name: limitPendingSignals, option: "i"},
	{name: limitLockedMemory, option: "l"},
	{name: limitMaxMemory, option: "m"},
	{name: limitOpenFiles, option: "n"},
	{name: limitPipeSize, option: "p"},
	{name: limitMessageQueues, option: "q"},
	{name: limitRealtimePriority, option: "r"},
	{name: limitStackSize, option: "s"},
	{name: limitCPUTime, option: "t"},
	{name: limitMaxUserProcesses, option: "u"},
	{name: limitVirtualMemory, option: "v"},
	{name: limitFileLocks, option: "x"},
}

var kernelSettingPaths = []struct {
	name string
	path string
}{
	{name: kernelFileMax, path: "/proc/sys/fs/file-max"},
	{name: kernelMaxMapCount, path: "/proc/sys/vm/max_map_count"},
	{name: kernelUnprivilegedUserNS, path: "/proc/sys/kernel/unprivileged_userns_clone"},
	{name: kernelReceiveBufferMax, path: "/proc/sys/net/core/rmem_max"},
	{name: kernelSendBufferMax, path: "/proc/sys/net/core/wmem_max"},
	{name: kernelTCPReceiveBufferSize, path: "/proc/sys/net/ipv4/tcp_rmem"},
	{name: kernelTCPSendBufferSize, path: "/proc/sys/net/ipv4/tcp_wmem"},
}

type resourceLimitSnapshot map[string]string

type resourceLimitSnapshots struct {
	direct resourceLimitSnapshot
	nested resourceLimitSnapshot
}

type kernelSettingSnapshot map[string]string

type systemSettingsJob struct {
	job       framework.SbatchJob
	kind      string
	container probeContainer
}

// systemSettingsProbeResult is the raw output of a combined probe and the
// settings parsed from it.
type systemSettingsProbeResult struct {
	kind   string
	output string
	limits resourceLimitSnapshots
	kernel kernelSettingSnapshot
}

type SystemSettings struct {
	info     *framework.ClusterInfo
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector

	worker framework.WorkerInfo
	jobs   []systemSettingsJob

	limits resourceLimitSnapshots

	loginKernel  kernelSettingSnapshot
	workerKernel kernelSettingSnapshot
	nativeKernel kernelSettingSnapshot
	enrootKernel kernelSettingSnapshot
	dockerKernel kernelSettingSnapshot

	loginIdentitySet  bool
	regularUserKernel kernelSettingSnapshot

	interactive []systemSettingsProbeResult

	nvidiaLibraries []nvidiaLibraryJobResult
}

func NewSystemSettings(
	info *framework.ClusterInfo,
	runtime framework.Runtime,
	slurm *framework.SlurmClient,
	kubectl *framework.KubectlClient,
	selector *framework.WorkerSelector,
) *SystemSettings {
	return &SystemSettings{
		info:     info,
		runtime:  runtime,
		slurm:    slurm,
		kubectl:  kubectl,
		selector: selector,
	}
}

func (s *SystemSettings) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^root collects direct and nested Bash resource limits over SSH to the login node$`, s.collectLoginResourceLimits)
	sc.Step(`^direct and nested login SSH resource limits match the expected default profile$`, s.loginLimitsMatchDefaultProfile)
	sc.Step(`^a healthy worker is selected for system-settings checks$`, s.selectHealthyWorker)
	sc.Step(`^root collects direct and nested Bash resource limits over SSH to the worker$`, s.collectWorkerResourceLimits)
	sc.Step(`^direct and nested worker SSH resource limits match the compute profile$`, s.resourceLimitsMatchComputeProfile)
	sc.Step(`^a regular user account is available on the worker$`, s.regularUserIsAvailableOnWorker)
	sc.Step(`^worker SSH without a job is allowed for the regular user$`, s.workerSSHWithoutJobIsAllowedForRegularUser)
	sc.Step(`^PAM Slurm adopt requires a job for the regular user$`, s.pamSlurmAdoptRequiresJobForRegularUser)
	sc.Step(`^the regular user starts a job on the worker and collects direct and nested Bash resource limits over SSH$`, s.collectAdoptedWorkerResourceLimits)
	sc.Step(`^direct and nested adopted SSH resource limits match the regular-user Slurm compute profile$`, s.resourceLimitsMatchRegularUserSlurmComputeProfile)
	sc.Step(`^a regular user has an SSH identity on the login node$`, s.regularUserHasLoginSSHIdentity)
	sc.Step(`^the regular user collects resource limits and kernel settings over SSH to the login node$`, s.collectRegularUserLoginSettings)
	sc.Step(`^the regular-user login SSH session exposes the required kernel settings$`, s.regularUserLoginExposesRequiredKernelSettings)
	sc.Step(`^the regular user runs native and Enroot interactive jobs that collect resource limits and kernel settings$`, s.runInteractiveJobs)
	sc.Step(`^both interactive jobs report their resource limits and kernel settings$`, s.interactiveJobsReportSettings)
	sc.Step(`^both interactive jobs' resource limits match the Slurm compute profile$`, s.interactiveJobsMatchSlurmComputeProfile)
	sc.Step(`^both interactive jobs expose the required kernel settings$`, s.interactiveJobsExposeRequiredKernelSettings)
	sc.Step(`^the regular user collects direct and nested Bash resource limits over SSH to the worker$`, s.collectRegularUserWorkerResourceLimits)
	sc.Step(`^direct and nested worker SSH resource limits match the regular-user compute profile$`, s.resourceLimitsMatchRegularUserComputeProfile)
	sc.Step(`^a native Slurm job collects direct and nested Bash resource limits$`, s.submitNativeResourceLimitsJob)
	sc.Step(`^the native system-settings job succeeds$`, s.resourceLimitsJobSucceeds)
	sc.Step(`^the regular user submits a native Slurm job that collects direct and nested Bash resource limits$`, s.submitRegularUserResourceLimitsJob)
	sc.Step(`^the native system-settings job succeeds as the regular user$`, s.resourceLimitsJobSucceedsAsRegularUser)
	sc.Step(`^its direct and nested resource limits match the Slurm compute profile$`, s.resourceLimitsMatchSlurmComputeProfile)
	sc.Step(`^its direct and nested resource limits match the compute profile$`, s.resourceLimitsMatchComputeProfile)
	sc.Step(`^an Enroot Slurm job collects direct and nested Bash resource limits$`, s.submitEnrootResourceLimitsJob)
	sc.Step(`^the Enroot system-settings job succeeds$`, s.resourceLimitsJobSucceeds)
	sc.Step(`^a Docker Slurm job collects direct and nested Bash resource limits$`, s.submitDockerResourceLimitsJob)
	sc.Step(`^the Docker system-settings job succeeds$`, s.resourceLimitsJobSucceeds)
	sc.Step(`^kernel settings are collected from login and worker SSH sessions$`, s.collectSSHKernelSettings)
	sc.Step(`^both SSH sessions expose the required kernel settings$`, s.sshSessionsExposeRequiredKernelSettings)
	sc.Step(`^native and Enroot Slurm jobs collect kernel settings$`, s.submitKernelSettingsJobs)
	sc.Step(`^both kernel-settings jobs succeed$`, s.kernelSettingsJobsSucceed)
	sc.Step(`^both jobs expose the required kernel settings$`, s.jobsExposeRequiredKernelSettings)
	sc.Step(`^a Docker Slurm job collects kernel settings$`, s.submitDockerKernelSettingsJob)
	sc.Step(`^the Docker kernel-settings job succeeds$`, s.dockerKernelSettingsJobSucceeds)
	sc.Step(`^it exposes the required kernel settings$`, s.dockerJobExposesRequiredKernelSettings)
	sc.Step(`^a healthy GPU worker is selected for system-settings checks$`, s.selectHealthyGPUWorker)
	sc.Step(`^a healthy Docker-enabled worker is selected for system-settings checks$`, s.selectHealthyDockerWorker)
	sc.Step(`^a healthy Docker-enabled GPU worker is selected for system-settings checks$`, s.selectHealthyDockerGPUWorker)
	sc.Step(`^its Slurm container exposes the required NVIDIA driver capabilities$`, s.slurmContainerExposesRequiredNVIDIADriverCapabilities)
	sc.Step(`^native and Enroot GPU jobs look up NVIDIA driver libraries for the required capabilities$`, s.submitNVIDIALibrariesJobs)
	sc.Step(`^both NVIDIA library lookup jobs succeed$`, s.nvidiaLibrariesJobsSucceed)
	sc.Step(`^both jobs find a non-placeholder NVIDIA driver library for every required capability$`, s.nvidiaLibrariesJobsFindNonPlaceholderLibraries)
	sc.Step(`^a Docker GPU job looks up NVIDIA driver libraries for the required capabilities$`, s.submitDockerNVIDIALibrariesJob)
	sc.Step(`^the Docker NVIDIA library lookup job succeeds$`, s.nvidiaLibrariesJobsSucceed)
	sc.Step(`^it finds a non-placeholder NVIDIA driver library for every required capability$`, s.nvidiaLibrariesJobsFindNonPlaceholderLibraries)
}

func (s *SystemSettings) CleanupAndReset(ctx context.Context) {
	for _, tracked := range s.jobs {
		if tracked.job.IsZero() {
			continue
		}
		if err := s.slurm.CancelJob(ctx, tracked.job.ID, systemSettingsCleanup); err != nil {
			reportCleanupErrorf(ctx, s.runtime, "cleanup: cancel %s job %s: %v", tracked.kind, tracked.job.ID, err)
		}
		if tracked.container.isDocker() && s.worker.Name != "" {
			// Cancelling the job kills the Docker client, not necessarily the container.
			name := systemSettingsDockerContainerName(tracked.job.ID)
			command := fmt.Sprintf("sudo docker rm -f %s >/dev/null 2>&1 || true", framework.ShellQuote(name))
			if _, err := s.runtime.Worker(s.worker).Run(ctx, command); err != nil {
				s.runtime.Logf("cleanup: remove Docker container %s on worker %s: %v", name, s.worker.Name, err)
			}
		}
	}

	if s.loginIdentitySet {
		if err := removeSSHTestIdentity(
			ctx,
			s.runtime,
			sshTestUserName,
			loginSSHKeyName,
			loginSSHKeyComment,
		); err != nil {
			s.runtime.Logf("cleanup: remove login SSH identity of %s: %v", sshTestUserName, err)
		}
	}

	s.worker = framework.WorkerInfo{}
	s.jobs = nil
	s.limits = resourceLimitSnapshots{}
	s.loginKernel = nil
	s.workerKernel = nil
	s.nativeKernel = nil
	s.enrootKernel = nil
	s.dockerKernel = nil
	s.loginIdentitySet = false
	s.regularUserKernel = nil
	s.interactive = nil
	s.nvidiaLibraries = nil
}

func (s *SystemSettings) selectHealthyWorker(ctx context.Context) error {
	workers, err := s.selector.PickWorkers(ctx, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]
	s.runtime.Logf("system settings: selected worker=%s", s.worker.Name)
	return nil
}

func (s *SystemSettings) selectHealthyGPUWorker(ctx context.Context) error {
	workers, err := s.selector.PickGPUWorkers(ctx, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]
	s.runtime.Logf("system settings: selected GPU worker=%s", s.worker.Name)
	return nil
}

func (s *SystemSettings) selectHealthyDockerWorker(ctx context.Context) error {
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	workers, err := snapshot.WithDocker().PickWorkers(1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]
	s.runtime.Logf("system settings: selected Docker worker=%s", s.worker.Name)
	return nil
}

func (s *SystemSettings) selectHealthyDockerGPUWorker(ctx context.Context) error {
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	workers, err := snapshot.WithDocker().PickGPUWorkers(1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]
	s.runtime.Logf("system settings: selected Docker GPU worker=%s", s.worker.Name)
	return nil
}

func (s *SystemSettings) collectLoginResourceLimits(ctx context.Context) error {
	output, err := s.runLoginSSH(ctx, pairedResourceLimitProbe())
	if err != nil {
		return err
	}
	s.limits, err = parseResourceLimitSnapshots(output)
	if err != nil {
		return fmt.Errorf("parse login SSH resource limits: %w", err)
	}
	return nil
}

func (s *SystemSettings) collectWorkerResourceLimits(ctx context.Context) error {
	if err := s.requireWorker(); err != nil {
		return err
	}
	output, err := s.runtime.Worker(s.worker).RunWithDefaultRetry(ctx, pairedResourceLimitProbe())
	if err != nil {
		return fmt.Errorf("collect resource limits over SSH from worker %s: %w", s.worker.Name, err)
	}
	s.limits, err = parseResourceLimitSnapshots(output)
	if err != nil {
		return fmt.Errorf("parse worker %s SSH resource limits: %w", s.worker.Name, err)
	}
	return nil
}

func (s *SystemSettings) regularUserIsAvailableOnWorker(ctx context.Context) error {
	if err := s.requireWorker(); err != nil {
		return err
	}
	if err := ensureSSHTestUser(ctx, s.runtime, sshTestUserName); err != nil {
		return err
	}
	if err := waitForSSHTestUserOnWorker(ctx, s.runtime, sshTestUserName, s.worker); err != nil {
		return err
	}
	// slurmctld must resolve the user before it accepts a job submitted as them.
	return waitForSSHTestUserOnController(ctx, s.runtime, sshTestUserName)
}

func (s *SystemSettings) collectRegularUserWorkerResourceLimits(ctx context.Context) error {
	if err := s.requireWorker(); err != nil {
		return err
	}
	output, err := runInternalSSHAsTestUser(
		ctx,
		s.runtime,
		s.worker.Name,
		systemSettingsSSHTimeout,
		pairedResourceLimitProbe(),
	)
	if err != nil {
		return fmt.Errorf("collect resource limits over SSH from worker %s as %s: %w", s.worker.Name, sshTestUserName, err)
	}
	s.limits, err = parseResourceLimitSnapshots(output)
	if err != nil {
		return fmt.Errorf("parse worker %s SSH resource limits of %s: %w", s.worker.Name, sshTestUserName, err)
	}
	return nil
}

func (s *SystemSettings) workerSSHWithoutJobIsAllowedForRegularUser(ctx context.Context) error {
	requiresJob, err := s.regularUserNeedsJobForWorkerSSH(ctx)
	if err != nil {
		return err
	}
	if requiresJob {
		s.runtime.Logf("system settings: PAM Slurm adopt requires a job for %s on workers, skipping scenario", sshTestUserName)
		return godog.ErrSkip
	}
	return nil
}

func (s *SystemSettings) pamSlurmAdoptRequiresJobForRegularUser(ctx context.Context) error {
	requiresJob, err := s.regularUserNeedsJobForWorkerSSH(ctx)
	if err != nil {
		return err
	}
	if !requiresJob {
		s.runtime.Logf("system settings: PAM Slurm adopt is disabled or exempts %s, skipping scenario", sshTestUserName)
		return godog.ErrSkip
	}
	return nil
}

func (s *SystemSettings) regularUserNeedsJobForWorkerSSH(ctx context.Context) (bool, error) {
	cluster, err := s.kubectl.SlurmCluster(ctx, s.info.SlurmClusterName)
	if err != nil {
		return false, err
	}
	if !cluster.PAMSlurmAdoptEnabled {
		return false, nil
	}
	output, err := s.runtime.Jail().RunWithDefaultRetry(ctx, "id -nG "+framework.ShellQuote(sshTestUserName))
	if err != nil {
		return false, fmt.Errorf("list groups of %s: %w", sshTestUserName, err)
	}
	return pamSlurmAdoptRequiresJob(cluster, sshTestUserName, strings.Fields(output)), nil
}

func (s *SystemSettings) collectAdoptedWorkerResourceLimits(ctx context.Context) error {
	if err := s.requireWorker(); err != nil {
		return err
	}
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      adoptedSSHHolderJobName,
		Nodes:        1,
		Nodelist:     []string{s.worker.Name},
		TasksPerNode: 1,
		Wrap:         "sleep 3600",
		RunAsUser:    sshTestUserName,
	})
	if err != nil {
		return err
	}
	s.trackJob(job, adoptedSSHHolderJobName, probeContainerNone)
	s.runtime.Logf("system settings: submitted job=%s id=%s worker=%s user=%s",
		adoptedSSHHolderJobName, job.ID, s.worker.Name, sshTestUserName)
	if err := s.slurm.WaitForJobRunning(ctx, job.ID, systemSettingsJobTimeout); err != nil {
		return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, job, err)
	}

	output, err := runInternalSSHAsTestUser(
		ctx,
		s.runtime,
		s.worker.Name,
		systemSettingsSSHTimeout,
		pairedResourceLimitProbe(),
	)
	if err != nil {
		return fmt.Errorf("collect resource limits over SSH adopted into job %s on worker %s: %w", job.ID, s.worker.Name, err)
	}
	s.limits, err = parseResourceLimitSnapshots(output)
	if err != nil {
		return fmt.Errorf("parse adopted SSH resource limits on worker %s: %w", s.worker.Name, err)
	}
	return nil
}

func (s *SystemSettings) regularUserHasLoginSSHIdentity(ctx context.Context) error {
	if err := ensureSSHTestUser(ctx, s.runtime, sshTestUserName); err != nil {
		return err
	}
	s.loginIdentitySet = true
	return ensureSSHTestIdentity(ctx, s.runtime, sshTestUserName, loginSSHKeyName, loginSSHKeyComment)
}

func (s *SystemSettings) collectRegularUserLoginSettings(ctx context.Context) error {
	output, err := runSSHCommand(
		ctx,
		s.runtime,
		sshTestUserName,
		loginSSHKeyName,
		"localhost",
		systemSettingsSSHTimeout,
		resourceLimitsAndKernelSettingsProbe(),
	)
	if err != nil {
		return fmt.Errorf("collect settings over SSH from login node as %s: %w", sshTestUserName, err)
	}
	s.limits, err = parseResourceLimitSnapshots(output)
	if err != nil {
		return fmt.Errorf("parse login SSH resource limits of %s: %w", sshTestUserName, err)
	}
	s.regularUserKernel, err = parseKernelSettings(output)
	if err != nil {
		return fmt.Errorf("parse login SSH kernel settings of %s: %w", sshTestUserName, err)
	}
	return nil
}

func (s *SystemSettings) regularUserLoginExposesRequiredKernelSettings() error {
	return validateKernelSettings("regular-user login SSH", s.regularUserKernel)
}

func (s *SystemSettings) runInteractiveJobs(ctx context.Context) error {
	if err := s.requireWorker(); err != nil {
		return err
	}
	for _, jobSpec := range []struct {
		kind      string
		container probeContainer
	}{
		{kind: "native", container: probeContainerNone},
		{kind: "Enroot", container: probeContainerEnroot},
	} {
		command := interactiveProbeCommand(
			sshTestUserName,
			s.worker.Name,
			resourceLimitsAndKernelSettingsProbe(),
			jobSpec.container,
		)
		output, err := s.runtime.Jail().Run(ctx, command)
		if err != nil {
			return fmt.Errorf("run %s interactive job as %s on worker %s: %w; output:\n%s",
				jobSpec.kind, sshTestUserName, s.worker.Name, err, strings.TrimSpace(output))
		}
		s.interactive = append(s.interactive, systemSettingsProbeResult{kind: jobSpec.kind, output: output})
	}
	return nil
}

func (s *SystemSettings) interactiveJobsReportSettings() error {
	if len(s.interactive) != 2 {
		return fmt.Errorf("find interactive job outputs: got %d, want 2", len(s.interactive))
	}
	for i := range s.interactive {
		result := &s.interactive[i]
		var err error
		result.limits, err = parseResourceLimitSnapshots(result.output)
		if err != nil {
			return fmt.Errorf("parse resource limits from %s interactive job: %w; output:\n%s",
				result.kind, err, strings.TrimSpace(result.output))
		}
		result.kernel, err = parseKernelSettings(result.output)
		if err != nil {
			return fmt.Errorf("parse kernel settings from %s interactive job: %w; output:\n%s",
				result.kind, err, strings.TrimSpace(result.output))
		}
	}
	return nil
}

func (s *SystemSettings) interactiveJobsMatchSlurmComputeProfile() error {
	var validationErrors []error
	for _, result := range s.interactive {
		if err := validateSlurmResourceLimitSnapshots(result.limits, computeResourceLimitProfile()); err != nil {
			validationErrors = append(validationErrors, fmt.Errorf("validate %s interactive job: %w", result.kind, err))
		}
	}
	return errors.Join(validationErrors...)
}

func (s *SystemSettings) interactiveJobsExposeRequiredKernelSettings() error {
	var validationErrors []error
	for _, result := range s.interactive {
		validationErrors = append(validationErrors,
			validateKernelSettings(result.kind+" interactive job", result.kernel))
	}
	return errors.Join(validationErrors...)
}

func (s *SystemSettings) loginLimitsMatchDefaultProfile() error {
	return validateResourceLimitSnapshots(s.limits, loginResourceLimitProfile(), true)
}

func (s *SystemSettings) resourceLimitsMatchComputeProfile() error {
	return validateResourceLimitSnapshots(s.limits, computeResourceLimitProfile(), false)
}

func (s *SystemSettings) resourceLimitsMatchRegularUserComputeProfile() error {
	return validateResourceLimitSnapshots(s.limits, regularUserComputeResourceLimitProfile(), false)
}

func (s *SystemSettings) resourceLimitsMatchSlurmComputeProfile() error {
	return validateSlurmResourceLimitSnapshots(s.limits, computeResourceLimitProfile())
}

// resourceLimitsMatchRegularUserSlurmComputeProfile expects pam_limits to apply
// the regular-user profile to an adopted SSH session, while ulimit_pam_adopt
// sets max_memory from the job it was adopted into.
func (s *SystemSettings) resourceLimitsMatchRegularUserSlurmComputeProfile() error {
	return validateSlurmResourceLimitSnapshots(s.limits, regularUserComputeResourceLimitProfile())
}

func (s *SystemSettings) submitNativeResourceLimitsJob(ctx context.Context) error {
	return s.submitProbeJob(ctx, probeJobSpec{name: nativeResourceLimitsJobName, probe: pairedResourceLimitProbe()})
}

func (s *SystemSettings) submitEnrootResourceLimitsJob(ctx context.Context) error {
	return s.submitProbeJob(ctx, probeJobSpec{
		name:      enrootResourceLimitsJobName,
		probe:     pairedResourceLimitProbe(),
		container: probeContainerEnroot,
	})
}

func (s *SystemSettings) submitDockerResourceLimitsJob(ctx context.Context) error {
	return s.submitProbeJob(ctx, probeJobSpec{
		name:      dockerResourceLimitsJobName,
		probe:     pairedResourceLimitProbe(),
		container: probeContainerDocker,
	})
}

func (s *SystemSettings) submitRegularUserResourceLimitsJob(ctx context.Context) error {
	return s.submitProbeJob(ctx, probeJobSpec{
		name:  regularUserResourceLimitsJobName,
		probe: pairedResourceLimitProbe(),
		user:  sshTestUserName,
	})
}

func (s *SystemSettings) submitProbeJob(ctx context.Context, spec probeJobSpec) error {
	if err := s.requireWorker(); err != nil {
		return err
	}
	probe := spec.probe
	if spec.user != "" {
		probe = jobUserProbe() + "; " + probe
	}
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      spec.name,
		Nodes:        1,
		Nodelist:     []string{s.worker.Name},
		GPUsPerNode:  spec.gpus,
		TasksPerNode: 1,
		Wrap:         srunProbeCommand(probe, spec.container),
		RunAsUser:    spec.user,
	})
	if err != nil {
		return err
	}
	s.trackJob(job, spec.name, spec.container)
	s.runtime.Logf("system settings: submitted job=%s id=%s worker=%s stdout=%s",
		spec.name, job.ID, s.worker.Name, job.StdoutPath)
	return nil
}

func (s *SystemSettings) resourceLimitsJobSucceeds(ctx context.Context) error {
	return s.completeResourceLimitsJob(ctx, "")
}

func (s *SystemSettings) resourceLimitsJobSucceedsAsRegularUser(ctx context.Context) error {
	return s.completeResourceLimitsJob(ctx, sshTestUserName)
}

// completeResourceLimitsJob waits for the latest job, parses its resource
// limits and, when expectedUser is set, checks the user its task ran as.
func (s *SystemSettings) completeResourceLimitsJob(ctx context.Context, expectedUser string) error {
	job, err := s.latestJob()
	if err != nil {
		return err
	}
	output, err := s.completeJob(ctx, job)
	if err != nil {
		return err
	}
	if expectedUser != "" {
		if err := validateJobUser(output, expectedUser); err != nil {
			return fmt.Errorf("validate job %s: %w", job.ID, err)
		}
	}
	s.limits, err = parseResourceLimitSnapshots(output)
	if err != nil {
		return fmt.Errorf("parse resource limits from job %s: %w", job.ID, err)
	}
	return nil
}

func (s *SystemSettings) collectSSHKernelSettings(ctx context.Context) error {
	if err := s.requireWorker(); err != nil {
		return err
	}
	loginOutput, err := s.runLoginSSH(ctx, kernelSettingsProbe())
	if err != nil {
		return err
	}
	s.loginKernel, err = parseKernelSettings(loginOutput)
	if err != nil {
		return fmt.Errorf("parse login SSH kernel settings: %w", err)
	}

	workerOutput, err := s.runtime.Worker(s.worker).RunWithDefaultRetry(ctx, kernelSettingsProbe())
	if err != nil {
		return fmt.Errorf("collect kernel settings over SSH from worker %s: %w", s.worker.Name, err)
	}
	s.workerKernel, err = parseKernelSettings(workerOutput)
	if err != nil {
		return fmt.Errorf("parse worker %s SSH kernel settings: %w", s.worker.Name, err)
	}
	return nil
}

func (s *SystemSettings) sshSessionsExposeRequiredKernelSettings() error {
	return errors.Join(
		validateKernelSettings("login SSH", s.loginKernel),
		validateKernelSettings("worker SSH", s.workerKernel),
	)
}

func (s *SystemSettings) submitKernelSettingsJobs(ctx context.Context) error {
	for _, jobSpec := range []struct {
		name      string
		container probeContainer
	}{
		{name: nativeKernelSettingsJobName, container: probeContainerNone},
		{name: enrootKernelSettingsJobName, container: probeContainerEnroot},
	} {
		if err := s.submitProbeJob(ctx, probeJobSpec{
			name:      jobSpec.name,
			probe:     kernelSettingsProbe(),
			container: jobSpec.container,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *SystemSettings) submitDockerKernelSettingsJob(ctx context.Context) error {
	return s.submitProbeJob(ctx, probeJobSpec{
		name:      dockerKernelSettingsJobName,
		probe:     kernelSettingsProbe(),
		container: probeContainerDocker,
	})
}

func (s *SystemSettings) dockerKernelSettingsJobSucceeds(ctx context.Context) error {
	job, err := s.latestJob()
	if err != nil {
		return err
	}
	output, err := s.completeJob(ctx, job)
	if err != nil {
		return err
	}
	s.dockerKernel, err = parseKernelSettings(output)
	if err != nil {
		return fmt.Errorf("parse kernel settings from Docker job %s: %w", job.ID, err)
	}
	return nil
}

func (s *SystemSettings) dockerJobExposesRequiredKernelSettings() error {
	return validateKernelSettings("Docker Slurm job", s.dockerKernel)
}

func (s *SystemSettings) kernelSettingsJobsSucceed(ctx context.Context) error {
	if len(s.jobs) != 2 {
		return fmt.Errorf("find kernel-settings jobs: got %d, want 2", len(s.jobs))
	}
	for _, tracked := range s.jobs {
		output, err := s.completeJob(ctx, tracked.job)
		if err != nil {
			return err
		}
		settings, err := parseKernelSettings(output)
		if err != nil {
			return fmt.Errorf("parse kernel settings from %s job %s: %w", tracked.kind, tracked.job.ID, err)
		}
		switch tracked.kind {
		case nativeKernelSettingsJobName:
			s.nativeKernel = settings
		case enrootKernelSettingsJobName:
			s.enrootKernel = settings
		default:
			return fmt.Errorf("identify kernel-settings job kind %q", tracked.kind)
		}
	}
	return nil
}

func (s *SystemSettings) jobsExposeRequiredKernelSettings() error {
	return errors.Join(
		validateKernelSettings("native Slurm job", s.nativeKernel),
		validateKernelSettings("Enroot Slurm job", s.enrootKernel),
	)
}

func (s *SystemSettings) slurmContainerExposesRequiredNVIDIADriverCapabilities(ctx context.Context) error {
	if err := s.requireWorker(); err != nil {
		return err
	}
	pod, err := s.kubectl.WorkerPodForSlurmNode(ctx, s.worker.Name)
	if err != nil {
		return err
	}
	value, err := s.kubectl.WorkerContainerEnvironmentVariable(
		ctx,
		pod,
		slurmdContainerName,
		nvidiaDriverCapabilitiesName,
	)
	if err != nil {
		return err
	}
	return validateNVIDIADriverCapabilities(value)
}

func (s *SystemSettings) submitNVIDIALibrariesJobs(ctx context.Context) error {
	for _, jobSpec := range []struct {
		name      string
		container probeContainer
	}{
		{name: nativeNVIDIALibrariesJobName, container: probeContainerNone},
		{name: enrootNVIDIALibrariesJobName, container: probeContainerEnrootGPU},
	} {
		if err := s.submitProbeJob(ctx, probeJobSpec{
			name:      jobSpec.name,
			probe:     nvidiaLibraryProbe(),
			container: jobSpec.container,
			gpus:      1,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *SystemSettings) submitDockerNVIDIALibrariesJob(ctx context.Context) error {
	return s.submitProbeJob(ctx, probeJobSpec{
		name:      dockerNVIDIALibrariesJobName,
		probe:     nvidiaLibraryProbe(),
		container: probeContainerDockerGPU,
		gpus:      1,
	})
}

// nvidiaLibrariesJobsSucceed waits for every NVIDIA library job submitted in
// the scenario and parses its library lookups.
func (s *SystemSettings) nvidiaLibrariesJobsSucceed(ctx context.Context) error {
	if len(s.jobs) == 0 {
		return fmt.Errorf("find submitted NVIDIA library jobs")
	}
	for _, tracked := range s.jobs {
		output, err := s.completeJob(ctx, tracked.job)
		if err != nil {
			return err
		}
		lookups, err := parseNVIDIALibraryLookups(output)
		if err != nil {
			return fmt.Errorf("parse NVIDIA library lookups from %s job %s: %w", tracked.kind, tracked.job.ID, err)
		}
		s.nvidiaLibraries = append(s.nvidiaLibraries, nvidiaLibraryJobResult{kind: tracked.kind, lookups: lookups})
	}
	return nil
}

func (s *SystemSettings) nvidiaLibrariesJobsFindNonPlaceholderLibraries() error {
	if len(s.nvidiaLibraries) == 0 {
		return fmt.Errorf("find NVIDIA library lookups")
	}
	var validationErrors []error
	for _, result := range s.nvidiaLibraries {
		validationErrors = append(validationErrors, validateNVIDIALibraryLookups(result.kind+" job", result.lookups))
	}
	return errors.Join(validationErrors...)
}

func (s *SystemSettings) runLoginSSH(ctx context.Context, command string) (string, error) {
	sshCommand := fmt.Sprintf(
		"ssh -o BatchMode=yes -o LogLevel=ERROR -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null %s %s",
		framework.ShellQuote("127.0.0.1"),
		framework.ShellQuote(command),
	)
	output, err := s.runtime.Jail().RunWithDefaultRetry(ctx, sshCommand)
	if err != nil {
		return "", fmt.Errorf("collect settings over SSH from login node: %w", err)
	}
	return output, nil
}

func (s *SystemSettings) requireWorker() error {
	if s.worker.Name == "" {
		return fmt.Errorf("select worker for system-settings checks")
	}
	return nil
}

func (s *SystemSettings) trackJob(job framework.SbatchJob, kind string, container probeContainer) {
	s.jobs = append(s.jobs, systemSettingsJob{job: job, kind: kind, container: container})
}

func (s *SystemSettings) latestJob() (framework.SbatchJob, error) {
	if len(s.jobs) == 0 || s.jobs[len(s.jobs)-1].job.IsZero() {
		return framework.SbatchJob{}, fmt.Errorf("find submitted system-settings job")
	}
	return s.jobs[len(s.jobs)-1].job, nil
}

func (s *SystemSettings) completeJob(ctx context.Context, job framework.SbatchJob) (string, error) {
	if err := framework.WaitForJobSucceeded(ctx, s.runtime, s.slurm, job, systemSettingsJobTimeout); err != nil {
		return "", err
	}
	output, err := readJobFile(ctx, s.runtime, job.StdoutPath)
	if err != nil {
		return "", err
	}
	s.untrackJob(job.ID)
	return output, nil
}

func (s *SystemSettings) untrackJob(jobID string) {
	for i := range s.jobs {
		if s.jobs[i].job.ID == jobID {
			s.jobs[i].job = framework.SbatchJob{}
			return
		}
	}
}

func resourceLimitProbe() string {
	lines := make([]string, 0, len(resourceLimitOptions))
	for _, limit := range resourceLimitOptions {
		lines = append(lines, fmt.Sprintf(
			"printf '%s=%%s\\n' \"$(ulimit -S -%s)\"",
			limit.name,
			limit.option,
		))
	}
	return strings.Join(lines, "; ")
}

func pairedResourceLimitProbe() string {
	probe := resourceLimitProbe()
	return fmt.Sprintf(
		"printf '%s\\n'; %s; printf '%s\\n'; bash -c %s",
		limitDirectMarker,
		probe,
		limitNestedMarker,
		framework.ShellQuote(probe),
	)
}

func kernelSettingsProbe() string {
	lines := make([]string, 0, len(kernelSettingPaths))
	for _, setting := range kernelSettingPaths {
		lines = append(lines, fmt.Sprintf(
			"printf '%s='; cat %s",
			setting.name,
			framework.ShellQuote(setting.path),
		))
	}
	return strings.Join(lines, "; ")
}

func jobUserProbe() string {
	return fmt.Sprintf("printf '%s=%%s\\n' \"$(id -un)\"", jobUserKey)
}

func srunProbeCommand(probe string, container probeContainer, flags ...string) string {
	capabilities := nvidiaDriverCapabilitiesName + "=" + strings.Join(requiredNVIDIADriverCapabilities(), ",")
	var args []string
	if container == probeContainerEnrootGPU {
		// Pyxis passes the job environment to the Enroot NVIDIA hook.
		args = append(args, capabilities)
	}
	args = append(args, "srun", "--nodes=1", "--ntasks=1")
	args = append(args, flags...)
	// The batch shell expands SLURM_JOB_ID, so cleanup can find a Docker container by job ID.
	dockerName := systemSettingsDockerContainerPrefix + "${SLURM_JOB_ID}"
	switch container {
	case probeContainerEnroot:
		args = append(args, "--container-image="+framework.ShellQuote(enrootLifecycleImage))
	case probeContainerEnrootGPU:
		args = append(args, "--container-image="+framework.ShellQuote(gpuSmokeEnrootImage))
	case probeContainerDocker:
		args = append(args, "docker", "run", "--rm", "--name", dockerName, framework.ShellQuote(ubuntuDockerImage))
	case probeContainerDockerGPU:
		args = append(args,
			"docker", "run", "--rm", "--name", dockerName,
			"--gpus=all", "-e", capabilities,
			framework.ShellQuote(ubuntuDockerImage),
		)
	}
	args = append(args, framework.BashLC(probe))
	return strings.Join(args, " ")
}

func resourceLimitsAndKernelSettingsProbe() string {
	return pairedResourceLimitProbe() + "; " + kernelSettingsProbe()
}

// interactiveProbeCommand runs probe as userName in an interactive srun --pty
// job on workerName. srun needs a terminal for --pty, which kubectl exec does
// not provide, so script allocates one.
func interactiveProbeCommand(userName, workerName, probe string, container probeContainer) string {
	srun := srunProbeCommand(probe, container,
		"--pty",
		fmt.Sprintf("--immediate=%d", int(interactiveJobImmediate.Seconds())),
		"--nodelist="+framework.ShellQuote(workerName),
	)
	interactive := fmt.Sprintf("timeout %d script -qec %s /dev/null",
		int(systemSettingsJobTimeout.Seconds()),
		framework.ShellQuote(srun),
	)
	return fmt.Sprintf("su - %s -c %s", framework.ShellQuote(userName), framework.ShellQuote(interactive))
}

// pamSlurmAdoptRequiresJob mirrors the worker PAM account stack: users and
// groups from the exemption lists bypass pam_slurm_adopt, everyone else needs
// a running job on the worker.
func pamSlurmAdoptRequiresJob(cluster framework.SlurmClusterInfo, userName string, userGroups []string) bool {
	if !cluster.PAMSlurmAdoptEnabled || slices.Contains(cluster.PAMSlurmAdoptExemptUsers, userName) {
		return false
	}
	for _, group := range userGroups {
		if slices.Contains(cluster.PAMSlurmAdoptExemptGroups, group) {
			return false
		}
	}
	return true
}

func systemSettingsDockerContainerName(jobID string) string {
	return systemSettingsDockerContainerPrefix + jobID
}

func validateJobUser(output, expected string) error {
	actual := parseKeyValueLine(output, jobUserKey)
	if actual != expected {
		return fmt.Errorf("check Slurm job task user %q: want %q", actual, expected)
	}
	return nil
}

func parseResourceLimitSnapshots(output string) (resourceLimitSnapshots, error) {
	snapshots := resourceLimitSnapshots{
		direct: make(resourceLimitSnapshot, len(resourceLimitOptions)),
		nested: make(resourceLimitSnapshot, len(resourceLimitOptions)),
	}
	known := make(map[string]struct{}, len(resourceLimitOptions))
	for _, limit := range resourceLimitOptions {
		known[limit.name] = struct{}{}
	}

	var current resourceLimitSnapshot
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case limitDirectMarker:
			current = snapshots.direct
			continue
		case limitNestedMarker:
			current = snapshots.nested
			continue
		}
		if current == nil {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if _, found := known[name]; !found {
			continue
		}
		if _, found := current[name]; found {
			return resourceLimitSnapshots{}, fmt.Errorf("parse duplicate resource limit %s", name)
		}
		current[name] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return resourceLimitSnapshots{}, fmt.Errorf("scan resource-limit output: %w", err)
	}
	for _, snapshot := range []struct {
		name   string
		limits resourceLimitSnapshot
	}{
		{name: "direct", limits: snapshots.direct},
		{name: "nested", limits: snapshots.nested},
	} {
		for _, limit := range resourceLimitOptions {
			if _, found := snapshot.limits[limit.name]; !found {
				return resourceLimitSnapshots{}, fmt.Errorf("find %s resource limit %s", snapshot.name, limit.name)
			}
		}
	}
	return snapshots, nil
}

func loginResourceLimitProfile() resourceLimitSnapshot {
	return resourceLimitSnapshot{
		limitRealtimeNonblocking: "unlimited",
		limitCoreFileSize:        "unlimited",
		limitDataSize:            "unlimited",
		limitSchedulingPriority:  "0",
		limitFileSize:            "unlimited",
		limitLockedMemory:        "8192",
		limitMaxMemory:           "unlimited",
		limitOpenFiles:           "1024",
		limitPipeSize:            "8",
		limitMessageQueues:       "819200",
		limitRealtimePriority:    "0",
		limitStackSize:           "8192",
		limitCPUTime:             "unlimited",
		limitMaxUserProcesses:    "unlimited",
		limitVirtualMemory:       "unlimited",
		limitFileLocks:           "unlimited",
	}
}

func computeResourceLimitProfile() resourceLimitSnapshot {
	profile := make(resourceLimitSnapshot, len(resourceLimitOptions))
	for _, limit := range resourceLimitOptions {
		profile[limit.name] = "unlimited"
	}
	profile[limitOpenFiles] = "1048576"
	profile[limitPipeSize] = "8"
	return profile
}

// regularUserComputeResourceLimitProfile is the compute profile of non-root
// worker SSH sessions. The worker limits.conf applies "* nice -20" only to
// non-root users, and pam_limits turns it into RLIMIT_NICE=20-(-20)=40.
func regularUserComputeResourceLimitProfile() resourceLimitSnapshot {
	profile := computeResourceLimitProfile()
	profile[limitSchedulingPriority] = "40"
	return profile
}

func validateResourceLimitProfile(actual, expected resourceLimitSnapshot, pendingSignalsPositive bool) error {
	var validationErrors []error
	for _, limit := range resourceLimitOptions {
		if pendingSignalsPositive && limit.name == limitPendingSignals {
			value, err := strconv.ParseUint(actual[limit.name], 10, 64)
			if err != nil || value == 0 {
				validationErrors = append(validationErrors, fmt.Errorf(
					"check resource limit %s=%q: want a positive integer",
					limit.name,
					actual[limit.name],
				))
			}
			continue
		}
		expectedValue, validate := expected[limit.name]
		if !validate {
			continue
		}
		if actual[limit.name] != expectedValue {
			validationErrors = append(validationErrors, fmt.Errorf(
				"check resource limit %s=%q: want %q",
				limit.name,
				actual[limit.name],
				expectedValue,
			))
		}
	}
	return errors.Join(validationErrors...)
}

func compareResourceLimitSnapshots(direct, nested resourceLimitSnapshot) error {
	var comparisonErrors []error
	for _, limit := range resourceLimitOptions {
		if direct[limit.name] != nested[limit.name] {
			comparisonErrors = append(comparisonErrors, fmt.Errorf(
				"check nested Bash resource limit %s=%q: direct shell has %q",
				limit.name,
				nested[limit.name],
				direct[limit.name],
			))
		}
	}
	return errors.Join(comparisonErrors...)
}

func validateResourceLimitSnapshots(
	snapshots resourceLimitSnapshots,
	expected resourceLimitSnapshot,
	pendingSignalsPositive bool,
) error {
	var directErr error
	if err := validateResourceLimitProfile(snapshots.direct, expected, pendingSignalsPositive); err != nil {
		directErr = fmt.Errorf("validate direct resource limits: %w", err)
	}
	var nestedErr error
	if err := validateResourceLimitProfile(snapshots.nested, expected, pendingSignalsPositive); err != nil {
		nestedErr = fmt.Errorf("validate nested Bash resource limits: %w", err)
	}
	var comparisonErr error
	if err := compareResourceLimitSnapshots(snapshots.direct, snapshots.nested); err != nil {
		comparisonErr = fmt.Errorf("compare direct and nested Bash resource limits: %w", err)
	}
	return errors.Join(directErr, nestedErr, comparisonErr)
}

// validateSlurmResourceLimitSnapshots validates snapshots against profile,
// except for max_memory, which Slurm sets from the job memory.
func validateSlurmResourceLimitSnapshots(snapshots resourceLimitSnapshots, profile resourceLimitSnapshot) error {
	expected := maps.Clone(profile)
	delete(expected, limitMaxMemory)

	var validationErrors []error
	for name, snapshot := range map[string]resourceLimitSnapshot{
		"direct":      snapshots.direct,
		"nested Bash": snapshots.nested,
	} {
		if err := validateResourceLimitProfile(snapshot, expected, false); err != nil {
			validationErrors = append(validationErrors, fmt.Errorf("validate %s resource limits: %w", name, err))
		}
		value, err := strconv.ParseUint(snapshot[limitMaxMemory], 10, 64)
		if err != nil || value == 0 {
			validationErrors = append(validationErrors, fmt.Errorf(
				"check %s resource limit %s=%q: want a positive integer set from Slurm job memory",
				name,
				limitMaxMemory,
				snapshot[limitMaxMemory],
			))
		}
	}
	if err := compareResourceLimitSnapshots(snapshots.direct, snapshots.nested); err != nil {
		validationErrors = append(validationErrors, fmt.Errorf("compare direct and nested Bash resource limits: %w", err))
	}
	return errors.Join(validationErrors...)
}

func parseKernelSettings(output string) (kernelSettingSnapshot, error) {
	settings := make(kernelSettingSnapshot, len(kernelSettingPaths))
	known := make(map[string]struct{}, len(kernelSettingPaths))
	for _, setting := range kernelSettingPaths {
		known[setting.name] = struct{}{}
	}

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		name, value, found := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !found {
			continue
		}
		if _, found := known[name]; !found {
			continue
		}
		if _, found := settings[name]; found {
			return nil, fmt.Errorf("parse duplicate kernel setting %s", name)
		}
		settings[name] = strings.Join(strings.Fields(value), " ")
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan kernel-settings output: %w", err)
	}
	for _, setting := range kernelSettingPaths {
		if _, found := settings[setting.name]; !found {
			return nil, fmt.Errorf("find kernel setting %s", setting.name)
		}
	}
	return settings, nil
}

func validateKernelSettings(source string, actual kernelSettingSnapshot) error {
	var validationErrors []error
	for name, minimum := range map[string]uint64{
		kernelFileMax:     388067,
		kernelMaxMapCount: 65530,
	} {
		value, err := strconv.ParseUint(actual[name], 10, 64)
		if err != nil || value < minimum {
			validationErrors = append(validationErrors, fmt.Errorf(
				"check %s kernel setting %s=%q: want at least %d",
				source,
				name,
				actual[name],
				minimum,
			))
		}
	}
	for name, expected := range map[string]string{
		kernelUnprivilegedUserNS:   "1",
		kernelReceiveBufferMax:     "536870912",
		kernelSendBufferMax:        "536870912",
		kernelTCPReceiveBufferSize: "4096 131072 536870912",
		kernelTCPSendBufferSize:    "4096 16384 536870912",
	} {
		if actual[name] != expected {
			validationErrors = append(validationErrors, fmt.Errorf(
				"check %s kernel setting %s=%q: want %q",
				source,
				name,
				actual[name],
				expected,
			))
		}
	}
	return errors.Join(validationErrors...)
}

func requiredNVIDIADriverCapabilities() []string {
	capabilities := make([]string, 0, len(nvidiaCapabilityLibraries))
	for _, entry := range nvidiaCapabilityLibraries {
		capabilities = append(capabilities, entry.capability)
	}
	return capabilities
}

// nvidiaLibraryProbe resolves each capability library by soname through the
// dynamic linker cache and reports the resolved file size. It does not load
// the libraries.
func nvidiaLibraryProbe() string {
	var entries []string
	for _, entry := range nvidiaCapabilityLibraries {
		for _, library := range entry.libraries {
			entries = append(entries, entry.capability+":"+library)
		}
	}
	return fmt.Sprintf(`for entry in %s; do
capability="${entry%%%%:*}"
library="${entry#*:}"
path="$(ldconfig -p | awk -v library="$library" '$1 == library { print $NF; exit }')"
size=0
if [ -n "$path" ]; then size="$(stat -Lc %%s "$path" 2>/dev/null || echo 0)"; fi
printf '%s=%%s %%s %%s %%s\n' "$capability" "$library" "${path:--}" "$size"
done`, strings.Join(entries, " "), nvidiaLibraryKey)
}

func parseNVIDIALibraryLookups(output string) ([]nvidiaLibraryLookup, error) {
	var lookups []nvidiaLibraryLookup
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		value, found := strings.CutPrefix(strings.TrimSpace(scanner.Text()), nvidiaLibraryKey+"=")
		if !found {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) != 4 {
			return nil, fmt.Errorf("parse NVIDIA library lookup %q: want capability, library, path and size", value)
		}
		size, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse size in NVIDIA library lookup %q: %w", value, err)
		}
		path := fields[2]
		if path == "-" {
			path = ""
		}
		lookups = append(lookups, nvidiaLibraryLookup{
			capability: fields[0],
			library:    fields[1],
			path:       path,
			size:       size,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan NVIDIA library output: %w", err)
	}
	if len(lookups) == 0 {
		return nil, fmt.Errorf("find %s lines in output", nvidiaLibraryKey)
	}
	return lookups, nil
}

// validateNVIDIALibraryLookups requires every required capability to resolve
// at least one of its libraries to a file larger than a placeholder. It checks
// presence only: a corrupt library or one with missing dependencies still passes.
func validateNVIDIALibraryLookups(source string, lookups []nvidiaLibraryLookup) error {
	var validationErrors []error
	for _, entry := range nvidiaCapabilityLibraries {
		present := false
		var observed []string
		for _, lookup := range lookups {
			if lookup.capability != entry.capability {
				continue
			}
			if lookup.path == "" {
				observed = append(observed, lookup.library+" not found")
				continue
			}
			observed = append(observed, fmt.Sprintf("%s=%s (%d bytes)", lookup.library, lookup.path, lookup.size))
			if lookup.size >= nvidiaNonPlaceholderLibraryMinSize {
				present = true
			}
		}
		if !present {
			if len(observed) == 0 {
				observed = append(observed, "no lookups")
			}
			validationErrors = append(validationErrors, fmt.Errorf(
				"check %s NVIDIA %s capability: want a non-placeholder library of at least %d bytes, got %s",
				source,
				entry.capability,
				nvidiaNonPlaceholderLibraryMinSize,
				strings.Join(observed, ", "),
			))
		}
	}
	return errors.Join(validationErrors...)
}

func validateNVIDIADriverCapabilities(value string) error {
	actual := make(map[string]struct{})
	for _, capability := range strings.Split(value, ",") {
		capability = strings.TrimSpace(capability)
		if capability != "" {
			actual[capability] = struct{}{}
		}
	}
	var missing []string
	for _, required := range requiredNVIDIADriverCapabilities() {
		if _, found := actual[required]; !found {
			missing = append(missing, required)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"check %s=%q: missing required capabilities %s",
			nvidiaDriverCapabilitiesName,
			value,
			strings.Join(missing, ","),
		)
	}
	return nil
}
