package sharedsteps

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	ncclDebugOutputDir = "/opt/soperator-outputs/local/nccl_logs"
	ncclDebugStateDir  = "/tmp/nccl_debug"
	ncclDebugMountDir  = "/etc/enroot/mounts.d"
	ncclDebugWait      = 2 * time.Minute
	ncclDebugCleanup   = time.Minute
)

type NCCLDebug struct {
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector
	worker   framework.WorkerInfo
	pod      framework.WorkerPodInfo
	jobs     []*ncclDebugJob
}

type ncclDebugJob struct {
	job       framework.SbatchJob
	level     string
	directory string
	task      ncclDebugTask
	stdout    string
	log       string
	metadata  string
	active    ncclDebugResources
	remaining ncclDebugResources
}

type ncclDebugTask struct {
	host, job, step, logPath string
	elapsedSeconds           uint64
}

type ncclDebugResources struct {
	files     []string
	namespace bool
}

func (t ncclDebugTask) filePrefix() string {
	return t.host + "." + t.job + "." + t.step + "."
}

func NewNCCLDebug(runtime framework.Runtime, slurm *framework.SlurmClient, kubectl *framework.KubectlClient, selector *framework.WorkerSelector) *NCCLDebug {
	return &NCCLDebug{runtime: runtime, slurm: slurm, kubectl: kubectl, selector: selector}
}

func (s *NCCLDebug) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^an idle GPU worker is available for NCCL debug logging$`, s.selectWorker)
	sc.Step(`^the soperatorchecks user is available for NCCL debug jobs$`, s.checkUser)
	sc.Step(`^the soperatorchecks user runs short full-node NCCL jobs with INFO and WARN logging$`, s.runJobs)
	sc.Step(`^both NCCL debug jobs report correct results$`, s.checkResults)
	sc.Step(`^their NCCL diagnostic logs reflect the requested verbosity$`, s.checkLogs)
	sc.Step(`^temporary NCCL logging resources are released after each step$`, s.checkResources)
}

func (s *NCCLDebug) CleanupAndReset(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, ncclDebugCleanup)
	defer cancel()
	for _, run := range s.jobs {
		if !run.job.IsZero() {
			if err := s.slurm.CancelJob(ctx, run.job.ID, ncclDebugCleanup); err != nil {
				reportCleanupErrorf(ctx, s.runtime, "Cleanup: cancel NCCL debug job %s: %v", run.job.ID, err)
				continue
			}
			// The allocation is gone, so only worker-level files can remain.
			if _, err := strconv.ParseUint(run.job.ID, 10, 32); err == nil {
				command := ncclDebugRemoveResourcesCommand(run.job.ID)
				if _, err := s.runtime.WorkerPod(s.pod).Run(ctx, command); err != nil {
					reportCleanupErrorf(ctx, s.runtime, "Cleanup: remove NCCL debug resources for job %s: %v", run.job.ID, err)
				}
			}
		}
	}
	s.worker = framework.WorkerInfo{}
	s.pod = framework.WorkerPodInfo{}
	s.jobs = nil
}

func (s *NCCLDebug) selectWorker(ctx context.Context) error {
	workers, err := s.selector.Acquire(ctx, framework.WorkerRequirements{Kind: "GPU", Count: 1, Idle: true})
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]
	if s.worker.SlurmNode.GPUCount < 1 {
		return fmt.Errorf("inspect GPU worker %s: invalid configured GPU count %d", s.worker.Name, s.worker.SlurmNode.GPUCount)
	}
	s.pod, err = s.kubectl.WorkerPodForSlurmNode(ctx, s.worker.Name)
	return err
}

func (s *NCCLDebug) checkUser(ctx context.Context) error {
	if _, err := s.runtime.Jail().Run(ctx, "id "+framework.ShellQuote(soperatorChecksUserName)); err != nil {
		return err
	}
	if err := waitForSSHTestUserOnWorker(ctx, s.runtime, soperatorChecksUserName, s.worker); err != nil {
		return err
	}
	return waitForSSHTestUserOnController(ctx, s.runtime, soperatorChecksUserName)
}

func (s *NCCLDebug) runJobs(ctx context.Context) error {
	artifacts, ok := framework.ScenarioArtifacts(ctx)
	if !ok {
		return fmt.Errorf("prepare NCCL debug jobs: scenario artifact paths are unavailable")
	}
	for _, level := range []string{"INFO", "WARN"} {
		run := &ncclDebugJob{level: level}
		s.jobs = append(s.jobs, run)
		template := path.Join(artifacts.JailDir, "nccl-debug-"+strings.ToLower(level)+"-XXXXXX")
		directory, err := s.runtime.Jail().Run(ctx, "sudo -iu "+framework.ShellQuote(soperatorChecksUserName)+" -- mktemp -d "+framework.ShellQuote(template))
		if err != nil {
			return fmt.Errorf("prepare %s NCCL debug coordination: %w", level, err)
		}
		directory = strings.TrimSpace(directory)
		if !strings.HasPrefix(directory, strings.TrimSuffix(template, "XXXXXX")) || path.Clean(directory) != directory {
			return fmt.Errorf("parse NCCL debug coordination directory from %q", directory)
		}
		run.directory = directory
		if err := s.runJob(ctx, run); err != nil {
			return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, run.job,
				fmt.Errorf("run %s NCCL debug job on %s: %w", level, s.worker.Name, err))
		}
	}
	return nil
}

func (s *NCCLDebug) runJob(ctx context.Context, run *ncclDebugJob) error {
	defer s.saveJobArtifacts(ctx, run)
	// sudo's login shell rewrites inline sbatch bodies. Script files preserve
	// newlines and defer expansion of Slurm variables until the task executes.
	for name, script := range map[string]string{
		"step.sh":  ncclDebugTaskCommand(s.worker.SlurmNode.GPUCount, run.directory),
		"batch.sh": ncclDebugBatchCommand(run.level, run.directory),
	} {
		command := "printf '%s' " + framework.ShellQuote(base64.StdEncoding.EncodeToString([]byte(script))) +
			" | base64 --decode > " + framework.ShellQuote(path.Join(run.directory, name)) +
			" && chmod 0644 " + framework.ShellQuote(path.Join(run.directory, name))
		if _, err := s.runtime.Jail().Run(ctx, command); err != nil {
			return fmt.Errorf("write NCCL debug script %s: %w", name, err)
		}
	}
	var err error
	run.job, err = s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-nccl-debug-" + strings.ToLower(run.level),
		Nodes:        1,
		Nodelist:     []string{s.worker.Name},
		GPUsPerNode:  s.worker.SlurmNode.GPUCount,
		TasksPerNode: 1,
		ExtraFlags:   []string{"--time=5:00", "--export=ALL,SNCCLD_ENABLED=0"},
		RunAsUser:    soperatorChecksUserName,
		Wrap:         "/bin/bash -e " + framework.ShellQuote(path.Join(run.directory, "batch.sh")),
	})
	if err != nil {
		return err
	}
	s.runtime.Logf("NCCL debug: worker=%s GPUs=%d level=%s job=%s", s.worker.Name, s.worker.SlurmNode.GPUCount, run.level, run.job.ID)
	metadata, err := s.waitForFile(ctx, run, "task")
	run.metadata = metadata
	if err != nil {
		return err
	}
	run.task, err = parseNCCLDebugTask(metadata, run.job.ID, run.level)
	if err != nil {
		return err
	}
	s.runtime.Logf("NCCL debug: job=%s workload_duration=%ds", run.job.ID, run.task.elapsedSeconds)
	run.active, err = s.resources(ctx, run)
	if err != nil {
		return err
	}
	if _, err := s.runtime.Jail().Run(ctx, "touch -- "+framework.ShellQuote(path.Join(run.directory, "release-step"))); err != nil {
		return err
	}
	if _, err := s.waitForFile(ctx, run, "step-finished"); err != nil {
		return err
	}
	// Keep the allocation alive: namespace teardown must not hide leaked files.
	err = framework.WaitForWithJobAlive(ctx, s.runtime, s.slurm, run.job, "NCCL step logging resources removed", ncclDebugCleanup, time.Second,
		func(waitCtx context.Context) (bool, error) {
			remaining, err := s.resources(waitCtx, run)
			run.remaining = remaining
			if err != nil {
				return false, err
			}
			if remaining.namespace != run.active.namespace {
				return false, fmt.Errorf("inspect job %s cleanup: mount namespace changed before allocation exit", run.job.ID)
			}
			if len(remaining.files) != 0 {
				return false, fmt.Errorf("inspect job %s step %s cleanup: remaining resources %v", run.job.ID, run.task.step, remaining.files)
			}
			return true, nil
		})
	if err != nil {
		return err
	}
	log, err := s.runtime.WorkerPod(s.pod).Run(ctx, "cat -- "+framework.ShellQuote("/mnt/jail"+run.task.logPath))
	if err != nil {
		return fmt.Errorf("read NCCL log %s on %s: %w", run.task.logPath, s.worker.Name, err)
	}
	run.log = log
	if _, err := s.runtime.Jail().Run(ctx, "touch -- "+framework.ShellQuote(path.Join(run.directory, "release-job"))); err != nil {
		return err
	}
	if err := framework.WaitForJobSucceeded(ctx, s.runtime, s.slurm, run.job, ncclDebugWait); err != nil {
		return err
	}
	run.stdout, err = readJobFile(ctx, s.runtime, run.job.StdoutPath)
	return err
}

func ncclDebugTaskCommand(gpus int, directory string) string {
	return fmt.Sprintf(`test "$(id -un)" = %s
test "$(id -u)" -ne 0
SECONDS=0
all_reduce_perf -g %d -b 32M -e 32M -f 2 -w 1 -n 1
printf 'HOST=%%s\nJOB=%%s\nSTEP=%%s\nUSER=%%s\nUID=%%s\nDEBUG=%%s\nDEBUG_FILE=%%s\nELAPSED_SECONDS=%%s\n' "$(hostname)" "$SLURM_JOB_ID" "$SLURM_STEP_ID" "$(id -un)" "$(id -u)" "$NCCL_DEBUG" "$NCCL_DEBUG_FILE" "$SECONDS" > %s
mv -- %s %s
%s`, framework.ShellQuote(soperatorChecksUserName), gpus,
		framework.ShellQuote(path.Join(directory, "task.tmp")), framework.ShellQuote(path.Join(directory, "task.tmp")), framework.ShellQuote(path.Join(directory, "task")),
		framework.JobReleaseGate(path.Join(directory, "release-step"), ncclDebugWait))
}

func ncclDebugBatchCommand(level, directory string) string {
	return strings.Join([]string{
		"set -eu",
		"unset NCCL_DEBUG NCCL_DEBUG_FILE NCCL_DEBUG_SUBSYS NCCL_DEBUG_LEVELS",
		"srun --nodes=1 --ntasks=1 --nccld-enabled=1 --nccld-log-level=" + level +
			" --nccld-out-file=1 --nccld-out-stdout=0 --nccld-out-dir=" + framework.ShellQuote(ncclDebugOutputDir) +
			" /bin/bash -e " + framework.ShellQuote(path.Join(directory, "step.sh")),
		"printf 'finished\\n' > " + framework.ShellQuote(path.Join(directory, "step-finished")),
		framework.JobReleaseGate(path.Join(directory, "release-job"), ncclDebugWait),
	}, "\n")
}

func parseNCCLDebugTask(output, jobID, level string) (ncclDebugTask, error) {
	task := ncclDebugTask{
		host: parseKeyValueLine(output, "HOST"), job: parseKeyValueLine(output, "JOB"), step: parseKeyValueLine(output, "STEP"),
		logPath: parseKeyValueLine(output, "DEBUG_FILE"),
	}
	uid, err := strconv.ParseUint(parseKeyValueLine(output, "UID"), 10, 32)
	if err != nil || uid == 0 || parseKeyValueLine(output, "USER") != soperatorChecksUserName {
		return ncclDebugTask{}, fmt.Errorf("verify non-root NCCL task identity: %s", output)
	}
	if task.job != jobID || task.host == "" || path.Base(task.host) != task.host {
		return ncclDebugTask{}, fmt.Errorf("verify NCCL task placement for job %s: %s", jobID, output)
	}
	if _, err := strconv.ParseUint(task.step, 10, 32); err != nil {
		return ncclDebugTask{}, fmt.Errorf("parse NCCL task step ID %q: %w", task.step, err)
	}
	task.elapsedSeconds, err = strconv.ParseUint(parseKeyValueLine(output, "ELAPSED_SECONDS"), 10, 32)
	if err != nil {
		return ncclDebugTask{}, fmt.Errorf("parse NCCL workload duration: %w", err)
	}
	expected := path.Join(ncclDebugOutputDir, task.filePrefix()+"out")
	if parseKeyValueLine(output, "DEBUG") != level || task.logPath != expected {
		return ncclDebugTask{}, fmt.Errorf("verify NCCL logging environment: expected level=%s file=%s, got %s", level, expected, output)
	}
	return task, nil
}

func (s *NCCLDebug) waitForFile(ctx context.Context, run *ncclDebugJob, name string) (string, error) {
	var output string
	err := framework.WaitForWithJobAlive(ctx, s.runtime, s.slurm, run.job, "NCCL debug "+name, ncclDebugWait, time.Second,
		func(waitCtx context.Context) (bool, error) {
			file := framework.ShellQuote(path.Join(run.directory, name))
			var err error
			output, err = s.runtime.Jail().Run(waitCtx, "if [ -f "+file+" ]; then cat -- "+file+"; fi")
			return strings.TrimSpace(output) != "", err
		})
	return output, err
}

func (s *NCCLDebug) resources(ctx context.Context, run *ncclDebugJob) (ncclDebugResources, error) {
	output, err := s.runtime.WorkerPod(s.pod).Run(ctx, ncclDebugResourcesCommand(run.task))
	if err != nil {
		return ncclDebugResources{}, err
	}
	return parseNCCLDebugResources(output)
}

func parseNCCLDebugResources(output string) (ncclDebugResources, error) {
	var resources ncclDebugResources
	namespace := parseKeyValueLine(output, "NAMESPACE")
	if namespace != "present" && namespace != "absent" {
		return resources, fmt.Errorf("parse NCCL logging resource snapshot: missing namespace status in %q", output)
	}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if file, ok := strings.CutPrefix(line, "RESOURCE="); ok {
			resources.files = append(resources.files, file)
		}
	}
	slices.Sort(resources.files)
	resources.files = slices.Compact(resources.files)
	resources.namespace = namespace == "present"
	return resources, nil
}

func ncclDebugResourcesCommand(task ncclDebugTask) string {
	pattern := task.filePrefix() + "*"
	mount := path.Join(ncclDebugMountDir, "30-nccl-debug-"+task.job+"-"+task.step+".fstab")
	scan := "if [ -d " + ncclDebugStateDir + " ]; then find " + ncclDebugStateDir + " -maxdepth 1 -name " + framework.ShellQuote(pattern) + " -printf 'RESOURCE=%f\\n'; fi\n" +
		"for file in " + framework.ShellQuote(mount) + " " + framework.ShellQuote(mount+".lock") + "; do if [ -e \"$file\" ]; then printf 'RESOURCE=%s\\n' \"$file\"; fi; done"
	command := "chroot /mnt/jail /bin/bash -ec " + framework.ShellQuote(scan)
	namespace := framework.ShellQuote(path.Join(jobNamespacePath(task.job), ".ns/mnt"))
	return command + "\nif [ -e " + namespace + " ]; then printf 'NAMESPACE=present\\n'; nsenter --mount=" + namespace +
		" " + command + "; else printf 'NAMESPACE=absent\\n'; fi"
}

func ncclDebugRemoveResourcesCommand(jobID string) string {
	return "chroot /mnt/jail /bin/bash -ec " + framework.ShellQuote(
		"if [ -d "+ncclDebugStateDir+" ]; then find "+ncclDebugStateDir+" -maxdepth 1 -name "+framework.ShellQuote("*."+jobID+".*")+" -delete; fi\n"+
			"if [ -d "+ncclDebugMountDir+" ]; then find "+ncclDebugMountDir+" -maxdepth 1 -name "+framework.ShellQuote("30-nccl-debug-"+jobID+"-*.fstab*")+" -delete; fi")
}

func (s *NCCLDebug) checkResults() error {
	for _, run := range s.jobs {
		var correct int
		for _, line := range strings.Split(run.stdout, "\n") {
			if ncclCorrectLine.MatchString(strings.TrimSpace(line)) {
				correct++
			}
		}
		if correct != 1 {
			return fmt.Errorf("verify %s NCCL job %s: expected one correct result, found %d; stdout:\n%s", run.level, run.job.ID, correct, run.stdout)
		}
	}
	return nil
}

func (s *NCCLDebug) checkLogs() error {
	for _, run := range s.jobs {
		if err := validateNCCLDebugLog(run.level, run.log); err != nil {
			return fmt.Errorf("verify job %s log %s on %s: %w", run.job.ID, run.task.logPath, s.worker.Name, err)
		}
	}
	return nil
}

func validateNCCLDebugLog(level, output string) error {
	info := strings.Contains(output, "NCCL INFO ")
	if (level == "INFO" && !info) || (level == "WARN" && info) {
		return fmt.Errorf("verify %s verbosity: NCCL INFO content present=%t; log:\n%s", level, info, output)
	}
	return nil
}

func (s *NCCLDebug) checkResources() error {
	for _, run := range s.jobs {
		if len(run.remaining.files) != 0 {
			return fmt.Errorf("verify released NCCL resources for job %s: remaining %v", run.job.ID, run.remaining.files)
		}
		prefix := run.task.filePrefix()
		for _, suffix := range []string{"state", "user-init.lock", "task-init-privileged.lock", "task-init.lock"} {
			if !slices.Contains(run.active.files, prefix+suffix) {
				return fmt.Errorf("verify active NCCL resources for job %s step %s: missing %s in %v", run.job.ID, run.task.step, prefix+suffix, run.active.files)
			}
		}
	}
	return nil
}

func (s *NCCLDebug) saveJobArtifacts(ctx context.Context, run *ncclDebugJob) {
	artifacts, ok := framework.ScenarioArtifacts(ctx)
	if !ok || run.job.IsZero() {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	files := map[string]string{
		"nccl.log": run.log, "task.txt": run.metadata,
		"active-resources.txt":    fmt.Sprintf("namespace=%t\n%s", run.active.namespace, strings.Join(run.active.files, "\n")),
		"remaining-resources.txt": fmt.Sprintf("namespace=%t\n%s", run.remaining.namespace, strings.Join(run.remaining.files, "\n")),
	}
	if run.task.logPath != "" {
		if output, err := s.runtime.WorkerPod(s.pod).Run(ctx, "cat -- "+framework.ShellQuote("/mnt/jail"+run.task.logPath)); err == nil {
			files["nccl.log"] = output
		} else {
			s.runtime.Logf("Collect NCCL debug log %s: %v", run.task.logPath, err)
		}
	}
	for name, remote := range map[string]string{"stdout": run.job.StdoutPath, "stderr": run.job.StderrPath} {
		output, err := readJobFile(ctx, s.runtime, remote)
		if err != nil {
			s.runtime.Logf("Collect NCCL debug artifact %s: %v", remote, err)
		}
		files[name] = output
	}
	for name, output := range files {
		file := filepath.Join(artifacts.RunnerDir, strings.ToLower(run.level)+"-"+run.job.ID+"-"+name)
		if err := os.WriteFile(file, []byte(output), 0o644); err != nil {
			s.runtime.Logf("Write NCCL debug artifact %s: %v", file, err)
		}
	}
}
