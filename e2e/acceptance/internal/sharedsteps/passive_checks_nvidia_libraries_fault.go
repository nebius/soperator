package sharedsteps

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	nvidiaLibrariesReasonBase = "[software_problem] " + nvidiaLibrariesCheckName
	// The first line of every fake library. Removal unmounts only while it is on top, so the real driver
	// mount underneath can never be unmounted by mistake.
	nvidiaLibraryFaultMarker = "soperator-e2e-nvidia-libraries-fault"
	nvidiaLibraryFaultDir    = "/run/soperator-e2e"
	// Above nvidiaNonPlaceholderLibraryMinSize, so nvidia_libraries tries to load it and fails on the format.
	corruptNVIDIALibraryPadding = 4096

	// Time the fault needs before the next periodic HealthCheckProgram run: injecting it, submitting the
	// CPU-only job and the Prolog drain take about 20 s. A periodic run in that window would meet the
	// fault in gpu_health_check before nvidia_libraries.
	nvidiaLibraryFaultBudget         = 60 * time.Second
	nvidiaLibraryFaultRemovalTimeout = 2 * time.Minute
	prologFailureRequeueTimeout      = 3 * time.Minute
	prologFailureDrainTimeout        = 2 * time.Minute
	drainRecoveryTimeout             = 2 * time.Minute
)

var nvidiaLibraryTargetPattern = regexp.MustCompile(`^/usr/lib/[a-z0-9_]+-linux-gnu/(lib[a-z0-9_-]+)\.so\.[0-9][0-9.]*$`)

// nvidiaLibraryFault is a fake file bind-mounted over a driver library in one worker's slurmd mount
// namespace. Driver libraries are per-worker bind mounts with private propagation, so the shared jail
// and the other workers keep the real library.
type nvidiaLibraryFault struct {
	worker framework.WorkerInfo
	pod    framework.WorkerPodInfo
	soname string
	// target is the library file the soname resolves to, as seen inside the jail.
	target   string
	fakePath string
	// mounted stays true until removal is verified; cleanup removes the fake while it is set.
	mounted bool
}

// Slurm runs HealthCheckProgram on every node once per HealthCheckInterval. The fault is injected only
// when the next run is at least nvidiaLibraryFaultBudget away, so the Prolog, not a periodic run, meets it.
func (s *PassiveChecks) thePeriodicHealthCheckProgramHasJustCompletedOnTheSelectedGPUWorker(ctx context.Context) error {
	if s.gpuWorker.Name == "" {
		return fmt.Errorf("GPU worker is not selected")
	}
	configuration, err := s.slurm.Configuration(ctx)
	if err != nil {
		return err
	}
	interval, err := healthCheckInterval(configuration)
	if err != nil {
		return err
	}
	timeout, err := periodicHealthCheckWait(configuration)
	if err != nil {
		return err
	}
	since, err := workerClockNow(ctx, s.runtime, s.gpuWorker)
	if err != nil {
		return err
	}
	// A run that started too long ago leaves too little time; the run after it starts a whole interval later.
	for attempt := 1; attempt <= 2; attempt++ {
		output, err := s.waitForCheckRunnerOutputStartedSince(ctx, s.gpuWorker, periodicHealthCheckContext, since, timeout)
		if err != nil {
			return fmt.Errorf("wait for a periodic HealthCheckProgram run on %s: %w", s.gpuWorker.Name, err)
		}
		startedAt, err := checkRunnerStartedAt(output)
		if err != nil {
			return err
		}
		now, err := workerClockNow(ctx, s.runtime, s.gpuWorker)
		if err != nil {
			return err
		}
		left := startedAt.Add(interval).Sub(now)
		if left >= nvidiaLibraryFaultBudget {
			s.runtime.Logf("passive checks: next periodic HealthCheckProgram run on %s is due in %s", s.gpuWorker.Name, left.Round(time.Second))
			return nil
		}
		s.runtime.Logf("passive checks: only %s left before the next periodic HealthCheckProgram run on %s; waiting for that run", left.Round(time.Second), s.gpuWorker.Name)
		since = now
	}
	return fmt.Errorf("no periodic HealthCheckProgram run on %s left %s before the next one", s.gpuWorker.Name, nvidiaLibraryFaultBudget)
}

func (s *PassiveChecks) aLibraryIsReplacedWithAFaultyFileOnTheSelectedGPUWorkerOnly(ctx context.Context, soname, fault string) error {
	if s.gpuWorker.Name == "" {
		return fmt.Errorf("GPU worker is not selected")
	}
	if s.nvidiaLibraryFault != nil {
		return fmt.Errorf("an NVIDIA library fault is already active on %s", s.nvidiaLibraryFault.worker.Name)
	}
	corrupt := fault == "a corrupt file"
	pod, err := framework.NewKubectlClient(s.runtime).WorkerPodForSlurmNode(ctx, s.gpuWorker.Name)
	if err != nil {
		return fmt.Errorf("find worker pod for %s: %w", s.gpuWorker.Name, err)
	}
	resolved, err := s.runtime.WorkerPod(pod).RunWithDefaultRetry(ctx, resolveNVIDIALibraryCommand(soname))
	if err != nil {
		return fmt.Errorf("resolve %s in the jail of %s: %w", soname, s.gpuWorker.Name, err)
	}
	target, err := validateNVIDIALibraryTarget(soname, resolved)
	if err != nil {
		return err
	}
	// Record before mounting: cleanup must look for the fault even when the command ran but its result was lost.
	s.nvidiaLibraryFault = &nvidiaLibraryFault{
		worker:   s.gpuWorker,
		pod:      pod,
		soname:   soname,
		target:   target,
		fakePath: path.Join(nvidiaLibraryFaultDir, path.Base(target)),
		mounted:  true,
	}
	if _, err := s.runtime.WorkerPod(pod).Run(ctx, nvidiaLibraryFaultCommand(*s.nvidiaLibraryFault, corrupt)); err != nil {
		return fmt.Errorf("mount %s over %s on %s: %w", fault, target, s.gpuWorker.Name, err)
	}
	sizeOutput, err := s.runtime.WorkerPod(pod).RunWithDefaultRetry(ctx, nvidiaLibrarySizeCommand(soname))
	if err != nil {
		return fmt.Errorf("read size of %s in the jail of %s: %w", soname, s.gpuWorker.Name, err)
	}
	if size, want := strings.TrimSpace(sizeOutput), strconv.Itoa(fakeNVIDIALibrarySize(corrupt)); size != want {
		return fmt.Errorf("jail of %s shows %s with %s bytes, expected the %s-byte fake", s.gpuWorker.Name, soname, size, want)
	}
	s.runtime.Logf("passive checks: replaced %s (%s) with %s on %s", soname, target, fault, s.gpuWorker.Name)
	return nil
}

func (s *PassiveChecks) aCPUOnlySlurmJobIsSubmittedToTheSelectedGPUWorker(ctx context.Context) error {
	if s.gpuWorker.Name == "" {
		return fmt.Errorf("GPU worker is not selected")
	}
	// A CPU-only job skips gpu_health_check, so only nvidia_libraries meets the broken library in Prolog.
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-passive-nvidia-libraries-fault",
		Nodes:        1,
		Nodelist:     []string{s.gpuWorker.Name},
		TasksPerNode: 1,
		// DefMemPerNode is unlimited, so jobs without --mem take the whole node.
		Args: []string{"--mem=512M"},
		Wrap: "true",
	})
	if err != nil {
		return err
	}
	s.nvidiaFaultJob = job
	return nil
}

func (s *PassiveChecks) theCPUOnlyJobIsRequeuedWithoutRunning(ctx context.Context) error {
	if s.nvidiaFaultJob.IsZero() {
		return fmt.Errorf("CPU-only job is not captured")
	}
	if err := waitForJobRequeuedAfterPrologFailure(ctx, s.runtime, s.slurm, s.nvidiaFaultJob, prologFailureRequeueTimeout); err != nil {
		return err
	}
	if err := s.slurm.CancelJob(ctx, s.nvidiaFaultJob.ID, prologFailureRequeueTimeout); err != nil {
		return fmt.Errorf("cancel requeued job %s: %w", s.nvidiaFaultJob.ID, err)
	}
	s.nvidiaFaultJob = framework.SbatchJob{}
	return nil
}

func (s *PassiveChecks) theSelectedGPUWorkerIsDrainedByNVIDIALibrariesBecauseTheLibraryFailsInProlog(ctx context.Context, soname, failure string) error {
	if s.gpuWorker.Name == "" {
		return fmt.Errorf("GPU worker is not selected")
	}
	return s.slurm.WaitForNodeReasonContains(ctx, s.gpuWorker.Name, nvidiaLibrariesReason(soname, failure, "prolog"), prologFailureDrainTimeout)
}

func (s *PassiveChecks) theNVIDIALibraryFaultIsRemovedFromTheSelectedGPUWorker(ctx context.Context) error {
	if s.nvidiaLibraryFault == nil || !s.nvidiaLibraryFault.mounted {
		return fmt.Errorf("no NVIDIA library fault is active")
	}
	return s.removeNVIDIALibraryFault(ctx)
}

func (s *PassiveChecks) theSelectedGPUWorkerNoLongerHasTheNVIDIALibrariesReason(ctx context.Context) error {
	if s.gpuWorker.Name == "" {
		return fmt.Errorf("GPU worker is not selected")
	}
	return s.slurm.WaitForNodeReasonCleared(ctx, s.gpuWorker.Name, nvidiaLibrariesReasonBase, drainRecoveryTimeout)
}

func (s *PassiveChecks) theSelectedGPUWorkerIsUsableAfterNVIDIALibraries(ctx context.Context) error {
	if s.gpuWorker.Name == "" {
		return fmt.Errorf("GPU worker is not selected")
	}
	return s.slurm.WaitForNodeUsable(ctx, s.gpuWorker.Name, drainRecoveryTimeout)
}

// cleanupAndResetNVIDIALibraryFault removes a fault that is still mounted and then undrains the worker it
// drained. The step family is recreated for every scenario, so everything happens in this one hook; what
// cannot be restored is recorded as a cleanup failure with the commands to restore it by hand.
func (s *PassiveChecks) cleanupAndResetNVIDIALibraryFault(ctx context.Context) {
	if !s.nvidiaFaultJob.IsZero() {
		if err := s.slurm.CancelJob(ctx, s.nvidiaFaultJob.ID, prologFailureRequeueTimeout); err != nil {
			s.runtime.Logf("cleanup: cancel job %s: %v", s.nvidiaFaultJob.ID, err)
		}
	}
	s.nvidiaFaultJob = framework.SbatchJob{}
	fault := s.nvidiaLibraryFault
	if fault == nil {
		return
	}
	defer func() { s.nvidiaLibraryFault = nil }()
	err := s.removeNVIDIALibraryFault(ctx)
	if err == nil {
		err = s.recoverNVIDIALibrariesDrain(ctx, *fault)
	}
	if err != nil {
		s.runtime.Logf("cleanup: %v", err)
		framework.RecordWorkerCleanupError(ctx, err)
	}
}

// removeNVIDIALibraryFault retries the removal until the fake is verifiably gone: the probe could read the
// library, the marker is not on top, and the jail sees a real-sized library again.
func (s *PassiveChecks) removeNVIDIALibraryFault(ctx context.Context) error {
	fault := s.nvidiaLibraryFault
	if fault == nil || !fault.mounted {
		return nil
	}
	var lastErr error
	err := s.runtime.WaitFor(ctx, fmt.Sprintf("fake %s removed from %s", fault.target, fault.worker.Name), nvidiaLibraryFaultRemovalTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		lastErr = s.unmountNVIDIALibraryFault(waitCtx, *fault)
		return lastErr == nil, lastErr
	})
	if err != nil {
		return fmt.Errorf("%w; the fake may still be mounted: run `kubectl exec -n %s %s -c slurmd -- umount /mnt/jail%s` until the jail shows the real %s, then delete %s",
			err, framework.SoperatorNamespace, fault.pod.PodName, fault.target, fault.soname, fault.fakePath)
	}
	fault.mounted = false
	return nil
}

func (s *PassiveChecks) unmountNVIDIALibraryFault(ctx context.Context, fault nvidiaLibraryFault) error {
	if _, err := s.runtime.WorkerPod(fault.pod).Run(ctx, nvidiaLibraryFaultRemoveCommand(fault)); err != nil {
		return fmt.Errorf("remove the fake %s from %s: %w", fault.target, fault.worker.Name, err)
	}
	sizeOutput, err := s.runtime.WorkerPod(fault.pod).Run(ctx, nvidiaLibrarySizeCommand(fault.soname))
	if err != nil {
		return fmt.Errorf("verify %s on %s after removing the fake: %w", fault.soname, fault.worker.Name, err)
	}
	size, err := strconv.Atoi(strings.TrimSpace(sizeOutput))
	if err != nil || size <= nvidiaNonPlaceholderLibraryMinSize || size == fakeNVIDIALibrarySize(true) {
		return fmt.Errorf("verify %s on %s after removing the fake: jail shows %q bytes, expected the real library",
			fault.soname, fault.worker.Name, strings.TrimSpace(sizeOutput))
	}
	return nil
}

// recoverNVIDIALibrariesDrain undrains the worker only when this fault drained it, and only after a fresh
// HealthCheckProgram run shows nvidia_libraries passing: the run's exit status proves nothing, and any
// other nvidia_libraries drain is a genuine failure.
func (s *PassiveChecks) recoverNVIDIALibrariesDrain(ctx context.Context, fault nvidiaLibraryFault) error {
	reason := nvidiaLibrariesReasonPrefix(fault.soname)
	node, err := s.slurm.NodeInfo(ctx, fault.worker.Name)
	if err != nil {
		return fmt.Errorf("read state of %s: %w; it may still be drained by %s", fault.worker.Name, err, nvidiaLibrariesCheckName)
	}
	if !node.IsDrainedFor(reason) {
		return nil
	}
	since, err := workerClockNow(ctx, s.runtime, fault.worker)
	if err != nil {
		return fmt.Errorf("%w; not resuming %s", err, fault.worker.Name)
	}
	if err := runManualHCProgram(ctx, s.runtime, fault.worker); err != nil {
		return fmt.Errorf("%w; not resuming %s", err, fault.worker.Name)
	}
	output, err := s.waitForCheckRunnerOutputStartedSince(ctx, fault.worker, periodicHealthCheckContext, since, passiveHCProgramTimeout)
	if err != nil {
		return fmt.Errorf("no fresh passing HealthCheckProgram result on %s after the fake %s was removed: %w; not resuming it", fault.worker.Name, fault.soname, err)
	}
	if err := s.assertNVIDIALibrariesCheckPassed(ctx, fault.worker, periodicHealthCheckContext, output); err != nil {
		return fmt.Errorf("%s still fails after the fake %s was removed: %w; not resuming %s", nvidiaLibrariesCheckName, fault.soname, err, fault.worker.Name)
	}
	if err := s.slurm.ResumeNodeIfDrainedByReason(ctx, fault.worker.Name, reason); err != nil {
		return fmt.Errorf("resume %s drained by %s: %w", fault.worker.Name, nvidiaLibrariesCheckName, err)
	}
	return nil
}

func resolveNVIDIALibraryCommand(soname string) string {
	return fmt.Sprintf(`chroot /mnt/jail readlink -f "/usr/lib/$(uname -m)-linux-gnu/"%s`, framework.ShellQuote(soname))
}

func nvidiaLibrarySizeCommand(soname string) string {
	return fmt.Sprintf(`chroot /mnt/jail stat -L -c %%s "/usr/lib/$(uname -m)-linux-gnu/"%s`, framework.ShellQuote(soname))
}

// validateNVIDIALibraryTarget accepts only the versioned file of the soname's own library, so the fault can
// never be mounted over anything else.
func validateNVIDIALibraryTarget(soname, resolved string) (string, error) {
	target := strings.TrimSpace(resolved)
	match := nvidiaLibraryTargetPattern.FindStringSubmatch(target)
	stem, _, _ := strings.Cut(soname, ".so")
	if len(match) != 2 || match[1] != stem {
		return "", fmt.Errorf("%s resolves to %q, expected a versioned %s file in the jail's library directory", soname, target, stem)
	}
	return target, nil
}

func fakeNVIDIALibrarySize(corrupt bool) int {
	size := len(nvidiaLibraryFaultMarker) + 1
	if corrupt {
		size += corruptNVIDIALibraryPadding
	}
	return size
}

func nvidiaLibraryFaultCommand(fault nvidiaLibraryFault, corrupt bool) string {
	fake := framework.ShellQuote(fault.fakePath)
	var commands []string
	commands = append(commands,
		"set -e",
		"mkdir -p "+framework.ShellQuote(nvidiaLibraryFaultDir),
		fmt.Sprintf("printf '%%s\\n' %s > %s", framework.ShellQuote(nvidiaLibraryFaultMarker), fake),
	)
	if corrupt {
		commands = append(commands, fmt.Sprintf("head -c %d /dev/zero >> %s", corruptNVIDIALibraryPadding, fake))
	}
	commands = append(commands, fmt.Sprintf("mount --bind %s %s", fake, framework.ShellQuote("/mnt/jail"+fault.target)))
	return strings.Join(commands, "; ")
}

func nvidiaLibraryFaultRemoveCommand(fault nvidiaLibraryFault) string {
	return removeFakeLibraryCommand("/mnt/jail"+fault.target, fault.fakePath)
}

// removeFakeLibraryCommand unmounts the fake while its marker is on top and deletes its backing file only
// once the target is readable and the marker is gone. on_top returns 0 for the marker, 1 for any other
// content and 2 when the target cannot be read, so a failed read never passes for a restored library.
func removeFakeLibraryCommand(targetPath, fakePath string) string {
	target, fake, marker := framework.ShellQuote(targetPath), framework.ShellQuote(fakePath), framework.ShellQuote(nvidiaLibraryFaultMarker)
	return strings.Join([]string{
		fmt.Sprintf("t=%s; m=%s", target, marker),
		fmt.Sprintf(`on_top() { content=$(set -o pipefail; head -c %d -- "$t" | tr -d '\000') || return 2; [ "$content" = "$m" ]; }`, len(nvidiaLibraryFaultMarker)),
		`for attempt in 1 2 3; do on_top; rc=$?; [ "$rc" -eq 1 ] && break; [ "$rc" -eq 2 ] && { echo "cannot read $t" >&2; exit 1; }; umount -- "$t" || exit 1; done`,
		`on_top; rc=$?; [ "$rc" -eq 1 ] || { echo "cannot verify that the fake library is gone from $t" >&2; exit 1; }`,
		"rm -f -- " + fake,
	}, "; ")
}

func nvidiaLibrariesReason(soname, failure, contextName string) string {
	return fmt.Sprintf("%s%s [%s]", nvidiaLibrariesReasonPrefix(soname), failure, contextName)
}

func nvidiaLibrariesReasonPrefix(soname string) string {
	return fmt.Sprintf("%s: %s: ", nvidiaLibrariesReasonBase, soname)
}
