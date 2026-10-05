package sharedsteps

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	systemSettingsJobTimeout = 5 * time.Minute
	systemSettingsCleanup    = 3 * time.Minute

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

	slurmdContainerName          = "slurmd"
	nvidiaDriverCapabilitiesName = "NVIDIA_DRIVER_CAPABILITIES"
	nativeResourceLimitsJobName  = "e2e-system-limits-native"
	enrootResourceLimitsJobName  = "e2e-system-limits-enroot"
	nativeKernelSettingsJobName  = "e2e-system-kernel-native"
	enrootKernelSettingsJobName  = "e2e-system-kernel-enroot"
)

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
	job  framework.SbatchJob
	kind string
}

type SystemSettings struct {
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
}

func NewSystemSettings(
	runtime framework.Runtime,
	slurm *framework.SlurmClient,
	kubectl *framework.KubectlClient,
	selector *framework.WorkerSelector,
) *SystemSettings {
	return &SystemSettings{
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
	sc.Step(`^a native Slurm job collects direct and nested Bash resource limits$`, s.submitNativeResourceLimitsJob)
	sc.Step(`^the native system-settings job succeeds$`, s.resourceLimitsJobSucceeds)
	sc.Step(`^its direct and nested resource limits match the compute profile$`, s.resourceLimitsMatchComputeProfile)
	sc.Step(`^an Enroot Slurm job collects direct and nested Bash resource limits$`, s.submitEnrootResourceLimitsJob)
	sc.Step(`^the Enroot system-settings job succeeds$`, s.resourceLimitsJobSucceeds)
	sc.Step(`^kernel settings are collected from login and worker SSH sessions$`, s.collectSSHKernelSettings)
	sc.Step(`^both SSH sessions expose the required kernel settings$`, s.sshSessionsExposeRequiredKernelSettings)
	sc.Step(`^native and Enroot Slurm jobs collect kernel settings$`, s.submitKernelSettingsJobs)
	sc.Step(`^both kernel-settings jobs succeed$`, s.kernelSettingsJobsSucceed)
	sc.Step(`^both jobs expose the required kernel settings$`, s.jobsExposeRequiredKernelSettings)
	sc.Step(`^a healthy GPU worker is selected for system-settings checks$`, s.selectHealthyGPUWorker)
	sc.Step(`^its Slurm container exposes the required NVIDIA driver capabilities$`, s.slurmContainerExposesRequiredNVIDIADriverCapabilities)
}

func (s *SystemSettings) CleanupAndReset(ctx context.Context) {
	for _, tracked := range s.jobs {
		if tracked.job.IsZero() {
			continue
		}
		if err := s.slurm.CancelJob(ctx, tracked.job.ID, systemSettingsCleanup); err != nil {
			s.runtime.Logf("cleanup: cancel %s job %s: %v", tracked.kind, tracked.job.ID, err)
		}
	}

	s.worker = framework.WorkerInfo{}
	s.jobs = nil
	s.limits = resourceLimitSnapshots{}
	s.loginKernel = nil
	s.workerKernel = nil
	s.nativeKernel = nil
	s.enrootKernel = nil
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

func (s *SystemSettings) loginLimitsMatchDefaultProfile() error {
	return validateResourceLimitSnapshots(s.limits, loginResourceLimitProfile(), true)
}

func (s *SystemSettings) resourceLimitsMatchComputeProfile() error {
	return validateResourceLimitSnapshots(s.limits, computeResourceLimitProfile(), false)
}

func (s *SystemSettings) submitNativeResourceLimitsJob(ctx context.Context) error {
	return s.submitResourceLimitsJob(ctx, nativeResourceLimitsJobName, false)
}

func (s *SystemSettings) submitEnrootResourceLimitsJob(ctx context.Context) error {
	return s.submitResourceLimitsJob(ctx, enrootResourceLimitsJobName, true)
}

func (s *SystemSettings) submitResourceLimitsJob(ctx context.Context, jobName string, enroot bool) error {
	if err := s.requireWorker(); err != nil {
		return err
	}
	command := srunProbeCommand(pairedResourceLimitProbe(), enroot)
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      jobName,
		Nodes:        1,
		Nodelist:     []string{s.worker.Name},
		TasksPerNode: 1,
		Wrap:         command,
	})
	if err != nil {
		return err
	}
	s.trackJob(job, jobName)
	s.runtime.Logf("system settings: submitted job=%s id=%s worker=%s stdout=%s",
		jobName, job.ID, s.worker.Name, job.StdoutPath)
	return nil
}

func (s *SystemSettings) resourceLimitsJobSucceeds(ctx context.Context) error {
	job, err := s.latestJob()
	if err != nil {
		return err
	}
	output, err := s.completeJob(ctx, job)
	if err != nil {
		return err
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
	if err := s.requireWorker(); err != nil {
		return err
	}
	for _, jobSpec := range []struct {
		name   string
		enroot bool
	}{
		{name: nativeKernelSettingsJobName},
		{name: enrootKernelSettingsJobName, enroot: true},
	} {
		job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
			JobName:      jobSpec.name,
			Nodes:        1,
			Nodelist:     []string{s.worker.Name},
			TasksPerNode: 1,
			Wrap:         srunProbeCommand(kernelSettingsProbe(), jobSpec.enroot),
		})
		if err != nil {
			return err
		}
		s.trackJob(job, jobSpec.name)
		s.runtime.Logf("system settings: submitted job=%s id=%s worker=%s stdout=%s",
			jobSpec.name, job.ID, s.worker.Name, job.StdoutPath)
	}
	return nil
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

func (s *SystemSettings) trackJob(job framework.SbatchJob, kind string) {
	s.jobs = append(s.jobs, systemSettingsJob{job: job, kind: kind})
}

func (s *SystemSettings) latestJob() (framework.SbatchJob, error) {
	if len(s.jobs) == 0 || s.jobs[len(s.jobs)-1].job.IsZero() {
		return framework.SbatchJob{}, fmt.Errorf("find submitted system-settings job")
	}
	return s.jobs[len(s.jobs)-1].job, nil
}

func (s *SystemSettings) completeJob(ctx context.Context, job framework.SbatchJob) (string, error) {
	if err := waitForJobSucceeded(ctx, s.runtime, s.slurm, job, systemSettingsJobTimeout); err != nil {
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

func srunProbeCommand(probe string, enroot bool) string {
	var args []string
	args = append(args, "srun", "--nodes=1", "--ntasks=1")
	if enroot {
		args = append(args, "--container-image="+framework.ShellQuote(enrootLifecycleImage))
	}
	args = append(args, framework.BashLC(probe))
	return strings.Join(args, " ")
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
		if actual[limit.name] != expected[limit.name] {
			validationErrors = append(validationErrors, fmt.Errorf(
				"check resource limit %s=%q: want %q",
				limit.name,
				actual[limit.name],
				expected[limit.name],
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

func validateNVIDIADriverCapabilities(value string) error {
	actual := make(map[string]struct{})
	for _, capability := range strings.Split(value, ",") {
		capability = strings.TrimSpace(capability)
		if capability != "" {
			actual[capability] = struct{}{}
		}
	}
	var missing []string
	for _, required := range []string{"compute", "graphics", "utility", "video"} {
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
