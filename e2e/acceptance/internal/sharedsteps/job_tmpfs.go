package sharedsteps

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const jobTmpfsTimeout = 5 * time.Minute

// The probe keeps charged pages alive until a later step requests normal exit.
// Reading the job parent cgroup includes charges that outlive individual steps.
const jobTmpfsProbe = `import json, os, pathlib, re, subprocess, sys, time
paths = [pathlib.Path(p) for p in ("/mnt/memory", "/dev/shm", "/tmp")]
marker = sys.argv[1]
job = os.environ["SLURM_JOB_ID"]
relative = next(line.split(":", 2)[2] for line in pathlib.Path("/proc/self/cgroup").read_text().splitlines() if line.startswith("0::"))
cgroup = pathlib.Path("/sys/fs/cgroup") / relative.lstrip("/")
while not re.fullmatch(r"step_(?:[0-9]+|batch|extern|interactive)", cgroup.name):
    assert cgroup != pathlib.Path("/sys/fs/cgroup"), "job cgroup not visible: " + relative
    cgroup = cgroup.parent
cgroup = cgroup.parent
def memory():
    stats = dict(line.split() for line in (cgroup / "memory.stat").read_text().splitlines())
    return int((cgroup / "memory.current").read_text()), int(stats["shmem"])
block = b"x" * (1024 * 1024)
samples = []
for directory in paths:
    filesystem = subprocess.check_output(["stat", "-f", "-c", "%T", "--", str(directory)], text=True).strip()
    assert filesystem == "tmpfs", (str(directory), filesystem)
    assert not (directory / marker).exists(), "another job's marker is visible: " + str(directory)
    before = memory()
    (directory / marker).write_text(job)
    with (directory / (marker + ".data")).open("wb", buffering=0) as output:
        for _ in range(16):
            output.write(block)
    deadline = time.monotonic() + 10
    while True:
        after = memory()
        delta = [a - b for a, b in zip(after, before)]
        if min(delta) >= 12 * 1024 * 1024:
            break
        assert time.monotonic() < deadline, (str(directory), "memory.current/shmem charge", delta)
        time.sleep(0.1)
    samples.append({"path": str(directory), "memory_current_delta": delta[0], "shmem_delta": delta[1]})
print("TMPFS_READY=" + json.dumps({"task_cgroup": relative, "cgroup": str(cgroup), "devices": {str(p): p.stat().st_dev for p in paths}, "samples": samples}), flush=True)
deadline = time.monotonic() + 900
while not (paths[2] / (marker + ".finish")).exists():
    assert time.monotonic() < deadline, "timed out waiting for the completion step"
    time.sleep(0.2)
`

type jobTmpfsStatus struct {
	NamespaceInode uint64
	TaskCgroup     string            `json:"task_cgroup"`
	Cgroup         string            `json:"cgroup"`
	Devices        map[string]uint64 `json:"devices"`
}

type JobTmpfs struct {
	runtime framework.Runtime
	slurm   *framework.SlurmClient
	kubectl *framework.KubectlClient
	pod     framework.WorkerPodInfo
	marker  string
	jobs    []framework.SbatchJob
	status  []jobTmpfsStatus
}

func NewJobTmpfs(runtime framework.Runtime, slurm *framework.SlurmClient, kubectl *framework.KubectlClient) *JobTmpfs {
	return &JobTmpfs{runtime: runtime, slurm: slurm, kubectl: kubectl}
}

func (s *JobTmpfs) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^two tmpfs probe jobs run on the same worker$`, s.startJobs)
	sc.Step(`^each job shares its temporary files across steps but not with the other job$`, s.checkSteps)
	sc.Step(`^sbcast and srun broadcast files into each job's temporary filesystems$`, s.checkBroadcast)
	sc.Step(`^Pyxis can access the job temporary files$`, s.checkPyxis)
	sc.Step(`^the first tmpfs job exits normally and the second is cancelled$`, s.finishJobs)
	sc.Step(`^both job mount namespaces and memory cgroups are removed$`, s.checkCleanup)
}

func (s *JobTmpfs) CleanupAndReset(ctx context.Context) {
	cleanupCtx, cancel := context.WithTimeout(ctx, jobTmpfsTimeout)
	defer cancel()
	for _, job := range s.jobs {
		if err := s.slurm.CancelJob(cleanupCtx, job.ID, time.Minute); err != nil {
			s.runtime.Logf("Cleanup: cancel tmpfs probe job %s: %v", job.ID, err)
		}
	}
	s.pod = framework.WorkerPodInfo{}
	s.marker = ""
	s.jobs = nil
	s.status = nil
}

func (s *JobTmpfs) startJobs(ctx context.Context) error {
	output, err := s.runtime.Jail().Run(ctx, "sinfo -hN -p main -t idle,mix -o '%N %C'")
	if err != nil {
		return fmt.Errorf("select worker for concurrent tmpfs jobs: %w", err)
	}
	worker, err := selectJobTmpfsWorker(output)
	if err != nil {
		return err
	}
	s.pod, err = s.kubectl.WorkerPodForSlurmNode(ctx, worker)
	if err != nil {
		return err
	}
	s.marker = fmt.Sprintf("soperator-tmpfs-%d", time.Now().UnixNano())
	for range 2 {
		job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
			JobName:      "e2e-job-tmpfs",
			Nodes:        1,
			Nodelist:     []string{worker},
			TasksPerNode: 1,
			ExtraFlags:   []string{"--cpus-per-task=1", "--mem=512M", "--time=20"},
			Wrap:         "python3 -u -c " + framework.ShellQuote(jobTmpfsProbe) + " " + framework.ShellQuote(s.marker),
		})
		if err != nil {
			return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, job, err)
		}
		s.jobs = append(s.jobs, job)
		if err := s.slurm.WaitForJobRunning(ctx, job.ID, jobTmpfsTimeout); err != nil {
			return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, job, err)
		}
		var status jobTmpfsStatus
		err = framework.WaitForWithJobAlive(ctx, s.runtime, s.slurm, job, "tmpfs pages charged to the job", jobTmpfsTimeout, framework.DefaultPollInterval,
			func(waitCtx context.Context) (bool, error) {
				stdout, err := readJobFile(waitCtx, s.runtime, job.StdoutPath)
				if err != nil {
					return false, err
				}
				value := parseKeyValueLine(stdout, "TMPFS_READY")
				if value == "" {
					return false, nil
				}
				if err := json.Unmarshal([]byte(value), &status); err != nil {
					return false, fmt.Errorf("parse tmpfs probe status: %w", err)
				}
				return status.TaskCgroup != "" && status.Cgroup != "", nil
			})
		if err != nil {
			return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, job, err)
		}
		cgroup, err := jobTmpfsJobCgroup(status.TaskCgroup, job.ID)
		if err != nil {
			return err
		}
		if cgroup != status.Cgroup {
			return fmt.Errorf("verify job memory accounting cgroup: got %q, expected %q", status.Cgroup, cgroup)
		}
		output, err := s.runtime.WorkerPod(s.pod).Run(ctx,
			"stat -Lc '%i' "+framework.ShellQuote(s.namespacePath(job)+"/.ns/mnt"))
		if err != nil {
			return fmt.Errorf("verify running job %s namespace anchor: %w", job.ID, err)
		}
		status.NamespaceInode, err = strconv.ParseUint(strings.TrimSpace(output), 10, 64)
		if err != nil || status.NamespaceInode == 0 {
			return fmt.Errorf("parse job %s namespace inode from %q", job.ID, output)
		}
		s.status = append(s.status, status)
	}
	for _, directory := range []string{"/mnt/memory", "/dev/shm", "/tmp"} {
		if s.status[0].Devices[directory] == s.status[1].Devices[directory] {
			return fmt.Errorf("verify job filesystem isolation: concurrent jobs share %s", directory)
		}
		if _, err := s.runtime.WorkerPod(s.pod).Run(ctx, "test ! -e "+framework.ShellQuote("/mnt/jail"+directory+"/"+s.marker)); err != nil {
			return fmt.Errorf("verify job files are hidden outside its namespace at %s: %w", directory, err)
		}
	}
	return nil
}

func jobTmpfsJobCgroup(taskCgroup, jobID string) (string, error) {
	cleaned := path.Clean(taskCgroup)
	if cleaned != taskCgroup || !strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("validate tmpfs task cgroup %q", taskCgroup)
	}
	for current := cleaned; current != "/"; current = path.Dir(current) {
		if isDockerSlurmStepComponent(path.Base(current)) && dockerCgroupParentBelongsToJob(current+"/user", jobID) {
			return path.Join("/sys/fs/cgroup", path.Dir(current)), nil
		}
	}
	return "", fmt.Errorf("find job %s cgroup in %q", jobID, taskCgroup)
}

func selectJobTmpfsWorker(output string) (string, error) {
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		cpus := strings.Split(fields[1], "/")
		if len(cpus) != 4 {
			continue
		}
		idle, err := strconv.Atoi(cpus[1])
		if err == nil && idle >= 2 {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("select tmpfs worker: require a main-partition worker with two idle CPUs")
}

func (s *JobTmpfs) namespacePath(job framework.SbatchJob) string {
	return path.Join("/var/spool/slurmd/job-container", s.pod.SlurmNodeName, job.ID)
}

func (s *JobTmpfs) step(ctx context.Context, job framework.SbatchJob, command string) error {
	output, err := s.runtime.Jail().Run(ctx, "srun --jobid="+framework.ShellQuote(job.ID)+" --overlap -N1 -n1 -c1 "+command)
	if err != nil {
		return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, job,
			fmt.Errorf("run tmpfs verification step: %w; output: %s", err, strings.TrimSpace(output)))
	}
	return nil
}

func (s *JobTmpfs) checkJob(ctx context.Context, job framework.SbatchJob, status jobTmpfsStatus) error {
	probe := `import json, pathlib, sys
devices = json.loads(sys.argv[3])
for directory in ("/mnt/memory", "/dev/shm", "/tmp"):
    assert pathlib.Path(directory).stat().st_dev == devices[directory], "steps have different filesystems: " + directory
    marker = pathlib.Path(directory) / sys.argv[1]
    assert marker.read_text() == sys.argv[2], "another job overwrote " + str(marker)
    assert marker.with_name(marker.name + ".data").stat().st_size == 16 * 1024 * 1024
`
	devices, err := json.Marshal(status.Devices)
	if err != nil {
		return fmt.Errorf("encode job tmpfs devices: %w", err)
	}
	return s.step(ctx, job, "python3 -c "+framework.ShellQuote(probe)+" "+framework.ShellQuote(s.marker)+" "+framework.ShellQuote(job.ID)+" "+framework.ShellQuote(string(devices)))
}

func (s *JobTmpfs) checkSteps(ctx context.Context) error {
	for i, job := range s.jobs {
		if err := s.checkJob(ctx, job, s.status[i]); err != nil {
			return err
		}
	}
	return nil
}

func jobTmpfsBroadcastCommand(jobID, marker string) string {
	probe := `#!/bin/sh
set -eu
for directory in /mnt/memory /dev/shm /tmp; do
    test "$(cat "$directory/$1.broadcast")" = "$2"
done
test "$(stat -c %d "$0")" = "$(stat -c %d /tmp)"
`
	var commands []string
	commands = append(commands,
		"set -eu",
		"source_dir=$(mktemp -d)",
		`trap 'rm -rf -- "$source_dir"' EXIT`,
		"printf '%s' "+framework.ShellQuote(jobID)+` > "$source_dir/marker"`,
		"printf '%s' "+framework.ShellQuote(probe)+` > "$source_dir/probe"`,
		`chmod 755 "$source_dir/probe"`,
	)
	for _, directory := range []string{"/mnt/memory", "/dev/shm", "/tmp"} {
		commands = append(commands, "sbcast --jobid="+framework.ShellQuote(jobID)+
			` --force "$source_dir/marker" `+framework.ShellQuote(path.Join(directory, marker+".broadcast")))
	}
	commands = append(commands, "srun --jobid="+framework.ShellQuote(jobID)+" --overlap -N1 -n1 -c1 --send-libs=no --bcast="+
		framework.ShellQuote(path.Join("/tmp", marker+".executable"))+` "$source_dir/probe" `+
		framework.ShellQuote(marker)+" "+framework.ShellQuote(jobID))
	return strings.Join(commands, "\n")
}

func (s *JobTmpfs) checkBroadcast(ctx context.Context) error {
	for _, job := range s.jobs {
		output, err := s.runtime.Jail().Run(ctx, jobTmpfsBroadcastCommand(job.ID, s.marker))
		if err != nil {
			return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, job,
				fmt.Errorf("broadcast files into job %s tmpfs: %w; output: %s", job.ID, err, output))
		}
	}
	return nil
}

func (s *JobTmpfs) checkPyxis(ctx context.Context) error {
	job := s.jobs[0]
	probe := `for directory in /mnt/memory /dev/shm /tmp; do test "$(cat "$directory/$1")" = "$2" || exit 1; done`
	return s.step(ctx, job, "--container-image="+framework.ShellQuote(enrootLifecycleImage)+
		" --container-mounts=/mnt/memory:/mnt/memory,/tmp:/tmp,/dev/shm:/dev/shm /bin/sh -ec "+framework.ShellQuote(probe)+
		" sh "+framework.ShellQuote(s.marker)+" "+framework.ShellQuote(job.ID))
}

func (s *JobTmpfs) finishJobs(ctx context.Context) error {
	if err := s.step(ctx, s.jobs[0], "touch "+framework.ShellQuote("/tmp/"+s.marker+".finish")); err != nil {
		return err
	}
	if err := waitForJobSucceeded(ctx, s.runtime, s.slurm, s.jobs[0], jobTmpfsTimeout); err != nil {
		return err
	}
	if err := s.checkJob(ctx, s.jobs[1], s.status[1]); err != nil {
		return fmt.Errorf("verify surviving job after neighboring job cleanup: %w", err)
	}
	return s.slurm.CancelJob(ctx, s.jobs[1].ID, jobTmpfsTimeout)
}

func (s *JobTmpfs) checkCleanup(ctx context.Context) error {
	for i, job := range s.jobs {
		probe := `import itertools, os, pathlib, sys
base, cgroup, inode = sys.argv[1:]
namespace = "mnt:[" + inode + "]"
remaining = [p for p in (base, cgroup) if pathlib.Path(p).exists()]
proc = pathlib.Path("/proc")
for entry in itertools.chain(proc.glob("[0-9]*/ns/mnt"), proc.glob("[0-9]*/fd/*")):
    try:
        if os.readlink(entry) == namespace:
            remaining.append(str(entry))
    except (FileNotFoundError, ProcessLookupError):
        pass
print("remaining=" + repr(remaining) if remaining else "clean")
`
		command := "python3 -c " + framework.ShellQuote(probe) + " " + framework.ShellQuote(s.namespacePath(job)) +
			" " + framework.ShellQuote(s.status[i].Cgroup) + " " + strconv.FormatUint(s.status[i].NamespaceInode, 10)
		err := s.runtime.WaitFor(ctx, "job namespace and cgroup removed", jobTmpfsTimeout, framework.DefaultPollInterval,
			func(waitCtx context.Context) (bool, error) {
				output, err := s.runtime.WorkerPod(s.pod).Run(waitCtx, command)
				if err != nil {
					return false, err
				}
				if strings.TrimSpace(output) != "clean" {
					s.runtime.Logf("Job %s tmpfs cleanup: %s", job.ID, strings.TrimSpace(output))
					return false, nil
				}
				return true, nil
			})
		if err != nil {
			return err
		}
	}
	return nil
}
