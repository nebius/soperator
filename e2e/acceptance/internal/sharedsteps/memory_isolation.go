package sharedsteps

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	memoryJobLimit        = int64(1024 * 1024 * 1024)
	memoryNeighborHeld    = int64(128 * 1024 * 1024)
	memoryImageSetupLimit = int64(4 * 1024 * 1024 * 1024)
	memoryStartTimeout    = 10 * time.Minute
	memoryOOMTimeout      = 2 * time.Minute
	memoryCleanupTimeout  = 3 * time.Minute
	memoryImage           = "cr.eu-north1.nebius.cloud/ml-containers/neubuntu:noble-20260113164317"
	memoryEnrootImage     = "docker://cr.eu-north1.nebius.cloud#ml-containers/neubuntu:noble-20260113164317"
)

//go:embed memory_probe.py
var memoryProbe string

//go:embed memory_host_probe.py
var memoryHostProbe string

type memorySnapshot struct {
	Cgroup     string            `json:"cgroup"`
	Maximum    string            `json:"maximum"`
	Current    int64             `json:"current"`
	Events     map[string]int64  `json:"events"`
	Swap       bool              `json:"swap"`
	SwapLimits map[string]string `json:"swap_limits"`
}

type memoryBatch struct {
	JobID      string `json:"job_id"`
	TaskCgroup string `json:"task_cgroup"`
	Hostname   string `json:"hostname"`
	Kernel     string `json:"kernel"`
}

type memoryWorkload struct {
	PID           int     `json:"pid"`
	StartTicks    string  `json:"start_ticks"`
	Allocated     int64   `json:"allocated"`
	HeldSeconds   float64 `json:"held_seconds"`
	Checksum      string  `json:"checksum"`
	Challenge     string  `json:"challenge"`
	TaskCgroup    string  `json:"task_cgroup"`
	PythonVersion string  `json:"python_version"`
}

type memoryJob struct {
	framework.SbatchJob
	Batch  memoryBatch
	Cgroup string
	Ready  memoryWorkload
}

type memoryObservation struct {
	Before      memorySnapshot `json:"before"`
	After       memorySnapshot `json:"after"`
	SampledPeak int64          `json:"sampled_peak"`
	Finished    bool           `json:"finished"`
	TimedOut    bool           `json:"timed_out"`
	Removed     bool           `json:"removed"`
	Error       string         `json:"error"`
}

type memoryHostProcess struct {
	Command    string `json:"command"`
	StartTicks string `json:"start_ticks"`
}

type memoryKernelMessage struct {
	Sequence int64  `json:"sequence"`
	Message  string `json:"message"`
}

type memoryHost struct {
	BootID        string                       `json:"boot_id"`
	Kernel        string                       `json:"kernel"`
	Available     int64                        `json:"available"`
	Processes     map[string]memoryHostProcess `json:"processes"`
	FirstSequence *int64                       `json:"first_sequence"`
	LastSequence  int64                        `json:"last_sequence"`
	OOMMessages   []memoryKernelMessage        `json:"oom_messages"`
}

type memoryHealth struct {
	Pod  corev1.Pod
	Node corev1.Node
}

type MemoryIsolation struct {
	info            *framework.ClusterInfo
	runtime         framework.Runtime
	slurm           *framework.SlurmClient
	kubectl         *framework.KubectlClient
	selector        *framework.WorkerSelector
	worker          framework.WorkerInfo
	pod             framework.WorkerPodInfo
	jobs            []memoryJob
	imageSetupJob   framework.SbatchJob
	execution       string
	workdir         string
	artifacts       framework.ScenarioArtifactPaths
	containerName   string
	containerLabel  string
	ownsReservation bool
	health          memoryHealth
	host            memoryHost
	neighbor        memorySnapshot
	report          map[string]any
}

func NewMemoryIsolation(info *framework.ClusterInfo, runtime framework.Runtime, slurm *framework.SlurmClient,
	kubectl *framework.KubectlClient, selector *framework.WorkerSelector) *MemoryIsolation {
	return &MemoryIsolation{info: info, runtime: runtime, slurm: slurm, kubectl: kubectl, selector: selector}
}

func (s *MemoryIsolation) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
		feature, _, _ := strings.Cut(scenario.Uri, ":")
		if !strings.HasSuffix(feature, "features/memory_isolation.feature") {
			return ctx, nil
		}
		// An existing, unacquired scope also belongs to the runner's finalization.
		if name, err := framework.WorkerReservationName(ctx); name == "" && err == nil {
			s.ownsReservation = true
			return framework.WithWorkerReservation(ctx, framework.NewReservationManager(), scenario.Name), nil
		}
		return ctx, nil
	})
	sc.Step(`^two memory-limited jobs run on the same worker using (native|Enroot|Docker)$`, s.startJobs)
	sc.Step(`^one job allocates memory below its limit$`, s.allocateBelow)
	sc.Step(`^that allocation succeeds within its job memory limit$`, s.checkBelow)
	sc.Step(`^the job allocates memory beyond its limit$`, s.allocateBeyond)
	sc.Step(`^an OOM kill is contained within that job$`, s.checkOOM)
	sc.Step(`^the neighboring job retains its memory and keeps running$`, s.checkNeighbor)
	sc.Step(`^worker and host services remain healthy$`, s.checkServices)
}

func (s *MemoryIsolation) startJobs(ctx context.Context, execution string) error {
	s.execution = execution
	s.report = map[string]any{"soperator_version": s.info.TargetSoperatorVersion, "execution": execution,
		"requested_memory_bytes": memoryJobLimit, "neighbor_held_bytes": memoryNeighborHeld, "image": memoryImage}
	artifacts, ok := framework.ScenarioArtifacts(ctx)
	if !ok {
		return fmt.Errorf("prepare memory isolation: scenario artifacts are required")
	}
	s.artifacts = artifacts
	// Jail artifacts survive CLI reruns; stale phase files must not start a new allocation.
	s.workdir = path.Join(artifacts.JailDir, "memory-probe-"+rand.Text())
	s.report["workdir"] = s.workdir
	workers, err := s.selector.Acquire(ctx, framework.WorkerRequirements{Count: 1, Docker: execution == "Docker", Idle: true})
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]
	s.report["worker"] = s.worker
	if s.worker.SlurmNode.RealMemoryMiB < uint64(2*memoryJobLimit/(1024*1024)) {
		return fmt.Errorf("require two 1GiB jobs on worker %s: RealMemory=%dMiB", s.worker.Name, s.worker.SlurmNode.RealMemoryMiB)
	}
	s.pod, err = s.kubectl.WorkerPodForSlurmNode(ctx, s.worker.Name)
	if err != nil {
		return err
	}
	s.health, err = s.readHealth(ctx)
	if err != nil {
		return err
	}
	s.report["health_before"] = s.health
	s.host, err = s.readHost(ctx, -1)
	if err != nil {
		return err
	}
	s.report["host_before"] = s.host
	if err := validateMemoryHostBaseline(s.host); err != nil {
		return err
	}
	if execution == "Enroot" {
		if s.host.Available < memoryImageSetupLimit+memoryJobLimit {
			return fmt.Errorf("require 5GiB available host memory before bounded image preparation: available=%d", s.host.Available)
		}
		s.report["host_before_image_setup"] = s.host
		if err := s.prepareEnrootImage(ctx); err != nil {
			return err
		}
		after, err := s.readHost(ctx, s.host.LastSequence)
		if err != nil {
			return err
		}
		s.report["host_after_image_setup"] = after
		if err := validateMemoryHostIdentity(s.host, after); err != nil {
			return err
		}
		if len(after.OOMMessages) != 0 {
			return fmt.Errorf("verify image preparation did not cause OOM: %+v", after.OOMMessages)
		}
		if err := validateMemoryHostBaseline(after); err != nil {
			return err
		}
		s.host = after
		s.report["host_before"] = after
	}
	version, err := s.runtime.Jail().Run(ctx, "srun --version")
	if err != nil || strings.TrimSpace(version) == "" {
		return fmt.Errorf("read Slurm version: output=%q error=%v", version, err)
	}
	s.report["slurm_version"] = strings.TrimSpace(version)
	if execution != "native" {
		command := "enroot version"
		if execution == "Docker" {
			command = "docker version --format '{{json .Server}}'"
		}
		version, err := s.runtime.Worker(s.worker).Run(ctx, command)
		if err != nil {
			return fmt.Errorf("read %s runtime version: %w", execution, err)
		}
		s.report["container_runtime_version"] = strings.TrimSpace(version)
	}
	setup := "install -d -m 0755 -- " + framework.ShellQuote(s.workdir) + " && printf '%s' " +
		framework.ShellQuote(memoryProbe) + " > " + framework.ShellQuote(path.Join(s.workdir, "probe.py"))
	if _, err := s.runtime.Jail().Run(ctx, setup); err != nil {
		return fmt.Errorf("install memory isolation probe: %w", err)
	}
	if execution == "Docker" {
		s.containerLabel = rand.Text()
	}
	for _, role := range []string{"target", "neighbor"} {
		job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
			JobName: "e2e-memory-" + role, Nodes: 1, Nodelist: []string{s.worker.Name}, TasksPerNode: 1,
			Args: []string{"--cpus-per-task=1", "--mem=1G", "--no-requeue"}, TimeLimit: 15 * time.Minute,
			Wrap: memoryJobCommand(s.workdir, role, execution, s.containerLabel),
		})
		// Preserve a parsed ID even when submission returned an uncertain error.
		if !job.IsZero() {
			s.jobs = append(s.jobs, memoryJob{SbatchJob: job})
		}
		if err != nil {
			return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, job, err)
		}
		index := len(s.jobs) - 1
		if err := s.waitProbe(ctx, job, role+".ready.json", &s.jobs[index].Ready, memoryStartTimeout); err != nil {
			return err
		}
		if _, err := s.readJSON(ctx, role+".batch.json", &s.jobs[index].Batch); err != nil {
			return err
		}
		batch := s.jobs[index].Batch
		if batch.JobID != job.ID || batch.Hostname != s.worker.Name || batch.Kernel == "" {
			return fmt.Errorf("verify memory job %s identity on %s: %+v", job.ID, s.worker.Name, batch)
		}
		s.jobs[index].Cgroup, err = slurmJobCgroup(batch.TaskCgroup, job.ID)
		if err != nil {
			return err
		}
		process, err := s.locateWorkload(ctx, role, s.jobs[index].Ready.StartTicks)
		if err != nil {
			return err
		}
		s.report[role+"_worker_process"] = process
		actual, err := slurmJobCgroup(process.TaskCgroup, job.ID)
		if err != nil || actual != s.jobs[index].Cgroup {
			return fmt.Errorf("verify memory workload belongs to job %s: task_cgroup=%q expected=%s error=%v", job.ID, process.TaskCgroup, s.jobs[index].Cgroup, err)
		}
		state, err := s.snapshot(ctx, s.jobs[index].Cgroup)
		if err != nil {
			return err
		}
		s.report[role+"_initial"] = state
		if err := validateMemoryLimit(state); err != nil {
			return err
		}
		if role == "neighbor" {
			s.neighbor = state
		}
	}
	s.report["jobs"] = s.jobs
	if s.jobs[0].Cgroup == s.jobs[1].Cgroup {
		return fmt.Errorf("verify memory isolation: jobs %s and %s share cgroup %s", s.jobs[0].ID, s.jobs[1].ID, s.jobs[0].Cgroup)
	}
	if s.jobs[1].Ready.Allocated != memoryNeighborHeld || s.neighbor.Current < memoryNeighborHeld {
		return fmt.Errorf("verify neighboring job holds %d bytes: workload=%+v memory=%+v", memoryNeighborHeld, s.jobs[1].Ready, s.neighbor)
	}
	if execution == "Docker" {
		s.containerName = memoryContainerName(s.jobs[0].ID)
		output, err := s.runtime.Worker(s.worker).Run(ctx, "docker inspect --format '{{.HostConfig.CgroupParent}}|{{.State.Pid}}|{{.Image}}' "+framework.ShellQuote(s.containerName))
		if err != nil {
			return fmt.Errorf("inspect memory container: %w", err)
		}
		s.report["docker_identity"] = strings.TrimSpace(output)
		fields := strings.Split(strings.TrimSpace(output), "|")
		if len(fields) != 3 || fields[2] == "" || !strings.HasPrefix(fields[0], strings.TrimPrefix(s.jobs[0].Cgroup, "/sys/fs/cgroup")+"/") || !slurmCgroupParentBelongsToJob(fields[0], s.jobs[0].ID) {
			return fmt.Errorf("verify Docker memory container belongs to job %s: %q", s.jobs[0].ID, output)
		}
		pid, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || pid == 0 {
			return fmt.Errorf("read running memory container PID: %q", output)
		}
	}
	return nil
}

func (s *MemoryIsolation) prepareEnrootImage(ctx context.Context) error {
	if s.worker.SlurmNode.RealMemoryMiB < uint64(memoryImageSetupLimit/(1024*1024)) {
		return fmt.Errorf("require 4GiB for Enroot image preparation on worker %s", s.worker.Name)
	}
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName: "e2e-memory-image-setup", Nodes: 1, Nodelist: []string{s.worker.Name}, TasksPerNode: 1,
		Args: []string{"--cpus-per-task=2", "--mem=4G", "--no-requeue"}, TimeLimit: memoryStartTimeout,
		Wrap: "ENROOT_MAX_PROCESSORS=2 srun --nodes=1 --ntasks=1 --container-image=" + framework.ShellQuote(memoryEnrootImage) + " python3 --version",
	})
	s.imageSetupJob = job
	s.report["image_setup_job"] = job
	s.report["image_setup_memory_bytes"] = memoryImageSetupLimit
	if err != nil {
		return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, job, err)
	}
	if err := framework.WaitForJobSucceeded(ctx, s.runtime, s.slurm, job, memoryStartTimeout); err != nil {
		return err
	}
	s.imageSetupJob = framework.SbatchJob{}
	return nil
}

func memoryContainerName(jobID string) string { return "e2e-memory-" + jobID }

func memoryJobCommand(directory, role, execution, label string) string {
	probe := "python3 -u " + framework.ShellQuote(path.Join(directory, "probe.py"))
	identify := probe + " identify " + framework.ShellQuote(directory) + " " + framework.ShellQuote(role)
	payload := probe + " " + role + " " + framework.ShellQuote(directory) + " " + strconv.FormatInt(memoryJobLimit, 10)
	if role == "neighbor" {
		payload = probe + " neighbor " + framework.ShellQuote(directory) + " " + strconv.FormatInt(memoryNeighborHeld, 10)
		return identify + " && exec srun --nodes=1 --ntasks=1 " + payload
	}
	var container string
	switch execution {
	case "Enroot":
		container = "--container-image=" + framework.ShellQuote(memoryEnrootImage) + " --container-mounts=" + framework.ShellQuote(directory+":"+directory) + " "
	case "Docker":
		container = "docker run --name e2e-memory-${SLURM_JOB_ID} --label " + framework.ShellQuote("soperator.e2e.memory="+label) + " --volume " + framework.ShellQuote(directory+":"+directory) + " " + framework.ShellQuote(memoryImage) + " "
	}
	// Hold the batch shell briefly so the job's OOM counters survive step exit.
	return identify + " || exit $?\nsrun --nodes=1 --ntasks=1 " + container + payload +
		"\nresult=$?\nprintf '%s' \"$result\" > " + framework.ShellQuote(path.Join(directory, "target.exit")) +
		"\ntimeout 120 sh -c " + framework.ShellQuote("while [ ! -f "+framework.ShellQuote(path.Join(directory, "target.release"))+" ]; do sleep 0.2; done") +
		"\nexit \"$result\""
}

func (s *MemoryIsolation) allocateBelow(ctx context.Context) error {
	_, err := s.runtime.Jail().Run(ctx, "touch -- "+framework.ShellQuote(path.Join(s.workdir, "target.start")))
	return err
}

func (s *MemoryIsolation) checkBelow(ctx context.Context) error {
	var workload memoryWorkload
	if err := s.waitProbe(ctx, s.jobs[0].SbatchJob, "target.below.json", &workload, time.Minute); err != nil {
		return err
	}
	state, err := s.snapshot(ctx, s.jobs[0].Cgroup)
	if err != nil {
		return err
	}
	s.report["below_workload"], s.report["below_memory"] = workload, state
	if err := validateMemoryBelow(workload, s.jobs[0].Ready, state); err != nil {
		return err
	}
	return s.slurm.AssertJobRunningWithoutRestarts(ctx, s.jobs[1].ID)
}

func (s *MemoryIsolation) allocateBeyond(ctx context.Context) error {
	command := "nohup python3 " + framework.ShellQuote("/mnt/jail"+path.Join(s.workdir, "probe.py")) +
		" observe " + framework.ShellQuote("/mnt/jail"+s.workdir) + " " + framework.ShellQuote(s.jobs[0].Cgroup) + " " +
		strconv.FormatFloat(memoryOOMTimeout.Seconds(), 'f', 0, 64) +
		" >" + framework.ShellQuote("/mnt/jail"+path.Join(s.workdir, "observer.log")) + " 2>&1 </dev/null &"
	if _, err := s.runtime.WorkerPod(s.pod).Run(ctx, command); err != nil {
		return fmt.Errorf("start memory observer outside job %s: %w", s.jobs[0].ID, err)
	}
	if err := s.runtime.WaitFor(ctx, "memory observer ready outside job", time.Minute, time.Second,
		func(ctx context.Context) (bool, error) {
			var observation memoryObservation
			ready, err := s.readJSON(ctx, "observer.json", &observation)
			if err == nil && ready && (observation.Error != "" || observation.Finished) {
				return false, &framework.PermanentError{Err: fmt.Errorf("start memory observer: %+v", observation)}
			}
			return ready, err
		}); err != nil {
		return err
	}
	_, err := s.runtime.Jail().Run(ctx, "touch -- "+framework.ShellQuote(path.Join(s.workdir, "target.exceed")))
	return err
}

func (s *MemoryIsolation) checkOOM(ctx context.Context) error {
	var observation memoryObservation
	err := s.runtime.WaitFor(ctx, "job-scoped OOM evidence", memoryOOMTimeout+30*time.Second, time.Second,
		func(ctx context.Context) (bool, error) {
			output, err := s.runtime.Jail().Run(ctx, "if test -e "+framework.ShellQuote(path.Join(s.workdir, "target.bypassed.json"))+"; then echo bypassed; fi")
			if err != nil {
				return false, err
			}
			if strings.TrimSpace(output) != "" {
				return false, &framework.PermanentError{Err: fmt.Errorf("enforce job %s memory limit: allocator reached its finite ceiling", s.jobs[0].ID)}
			}
			ready, err := s.readJSON(ctx, "observer.json", &observation)
			return ready && observation.Finished, err
		})
	s.report["observation"] = observation
	if err != nil {
		return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.jobs[0].SbatchJob, err)
	}
	after, err := s.readHost(ctx, s.host.LastSequence)
	if err != nil {
		return err
	}
	s.report["host_after_oom"] = after
	if err := validateMemoryOOM(observation, s.jobs[0].Cgroup); err != nil {
		return err
	}
	if err := validateMemoryHost(s.host, after, s.jobs[0].Cgroup); err != nil {
		return err
	}
	if _, err := s.runtime.Jail().Run(ctx, "touch -- "+framework.ShellQuote(path.Join(s.workdir, "target.release"))); err != nil {
		return err
	}
	if err := s.slurm.WaitForJobGone(ctx, s.jobs[0].ID, memoryCleanupTimeout); err != nil {
		return err
	}
	var status framework.SlurmJobInfo
	err = s.runtime.WaitFor(ctx, "memory job terminal accounting", 2*time.Minute, framework.DefaultPollInterval,
		func(ctx context.Context) (bool, error) {
			var err error
			status, err = s.slurm.JobInfo(ctx, s.jobs[0].ID)
			return status.SacctFound && !status.IsAlive(), err
		})
	s.report["target_outcome"] = status
	if err != nil {
		return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.jobs[0].SbatchJob, err)
	}
	if status.CompletedSuccessfully() || !status.HasNoRestarts() {
		return fmt.Errorf("verify OOM job %s failed without restarting: %+v", status.ID, status)
	}
	return nil
}

func (s *MemoryIsolation) checkNeighbor(ctx context.Context) error {
	challenge := rand.Text()
	if _, err := s.runtime.Jail().Run(ctx, "printf '%s' "+framework.ShellQuote(challenge)+" >"+framework.ShellQuote(path.Join(s.workdir, "neighbor.challenge"))); err != nil {
		return err
	}
	var workload memoryWorkload
	if err := s.waitProbe(ctx, s.jobs[1].SbatchJob, "neighbor.verified.json", &workload, time.Minute); err != nil {
		return err
	}
	after, err := s.snapshot(ctx, s.jobs[1].Cgroup)
	if err != nil {
		return err
	}
	s.report["neighbor_verified"], s.report["neighbor_after"] = workload, after
	if err := validateMemoryNeighbor(s.jobs[1].Ready, workload, challenge, s.neighbor, after); err != nil {
		return err
	}
	return s.slurm.AssertJobRunningWithoutRestarts(ctx, s.jobs[1].ID)
}

func (s *MemoryIsolation) checkServices(ctx context.Context) error {
	health, err := s.readHealth(ctx)
	if err != nil {
		return err
	}
	s.report["health_after"] = health
	if err := validateMemoryHealth(s.health, health); err != nil {
		return err
	}
	host, err := s.readHost(ctx, s.host.LastSequence)
	if err != nil {
		return err
	}
	s.report["host_after"] = host
	if err := validateMemoryHost(s.host, host, s.jobs[0].Cgroup); err != nil {
		return err
	}
	return s.slurm.CancelJob(ctx, s.jobs[1].ID, memoryCleanupTimeout)
}

func (s *MemoryIsolation) readHealth(ctx context.Context) (memoryHealth, error) {
	var health memoryHealth
	if err := s.kubectl.GetJSON(ctx, &health.Pod, "get", "pod", s.pod.PodName, "-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
		return health, err
	}
	if err := s.kubectl.GetJSON(ctx, &health.Node, "get", "node", s.pod.KubernetesNodeName, "-o", "json"); err != nil {
		return health, err
	}
	if health.Pod.DeletionTimestamp != nil || health.Pod.Status.Phase != corev1.PodRunning || len(health.Pod.Status.ContainerStatuses) == 0 || !kubeobjects.PodReady(health.Pod) || !k8sNodeReady(health.Node) {
		return health, fmt.Errorf("require healthy worker %s: pod=%s phase=%s node=%s", s.worker.Name, health.Pod.Name, health.Pod.Status.Phase, health.Node.Name)
	}
	return health, nil
}

func (s *MemoryIsolation) readHost(ctx context.Context, since int64) (memoryHost, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	probe := "python3 -c " + framework.ShellQuote(memoryHostProbe) + " " + strconv.FormatInt(since, 10)
	root, err := memoryHostRoot(s.health.Pod)
	if err != nil {
		return memoryHost{}, err
	}
	command := "chroot " + framework.ShellQuote(root) + " nsenter -t 1 -m -u -i -n bash -c " + framework.ShellQuote(probe)
	output, err := s.runtime.WorkerPod(s.pod).Run(ctx, command)
	if err != nil {
		return memoryHost{}, fmt.Errorf("inspect host processes and kernel OOM log on %s: %w", s.worker.Name, err)
	}
	var host memoryHost
	if err := json.Unmarshal([]byte(parseKeyValueLine(output, "MEMORY_HOST")), &host); err != nil {
		return host, fmt.Errorf("decode memory host evidence: %w; output=%s", err, output)
	}
	return host, nil
}

func memoryHostRoot(pod corev1.Pod) (string, error) {
	for _, container := range pod.Spec.Containers {
		if container.Name != "slurmd" {
			continue
		}
		for _, mount := range container.VolumeMounts {
			for _, volume := range pod.Spec.Volumes {
				if mount.Name == volume.Name && volume.HostPath != nil &&
					(volume.HostPath.Path == "/" || volume.HostPath.Path == "/run/nvidia/driver") {
					return mount.MountPath, nil
				}
			}
		}
	}
	return "", fmt.Errorf("locate host root mount in worker pod %s", pod.Name)
}

func (s *MemoryIsolation) snapshot(ctx context.Context, cgroup string) (memorySnapshot, error) {
	command := "python3 " + framework.ShellQuote("/mnt/jail"+path.Join(s.workdir, "probe.py")) + " snapshot " + framework.ShellQuote(cgroup)
	output, err := s.runtime.WorkerPod(s.pod).Run(ctx, command)
	if err != nil {
		return memorySnapshot{}, fmt.Errorf("inspect memory cgroup %s: %w", cgroup, err)
	}
	var state memorySnapshot
	if err := json.Unmarshal([]byte(output), &state); err != nil {
		return state, fmt.Errorf("decode memory cgroup %s: %w", cgroup, err)
	}
	return state, nil
}

func (s *MemoryIsolation) locateWorkload(ctx context.Context, role, startTicks string) (memoryWorkload, error) {
	command := "python3 " + framework.ShellQuote("/mnt/jail"+path.Join(s.workdir, "probe.py")) + " locate " +
		framework.ShellQuote(s.workdir) + " " + framework.ShellQuote(role) + " " + framework.ShellQuote(startTicks)
	output, err := s.runtime.WorkerPod(s.pod).Run(ctx, command)
	if err != nil {
		return memoryWorkload{}, fmt.Errorf("locate %s memory process from worker cgroup namespace: %w", role, err)
	}
	var process memoryWorkload
	if err := json.Unmarshal([]byte(output), &process); err != nil {
		return process, fmt.Errorf("decode worker memory process identity: %w", err)
	}
	if process.PID <= 0 || process.StartTicks == "" || process.StartTicks != startTicks {
		return process, fmt.Errorf("verify worker memory process identity: %+v", process)
	}
	return process, nil
}

func (s *MemoryIsolation) readJSON(ctx context.Context, name string, target any) (bool, error) {
	filename := framework.ShellQuote(path.Join(s.workdir, name))
	output, err := s.runtime.Jail().Run(ctx, "if test -s "+filename+"; then cat -- "+filename+"; fi")
	if err != nil || strings.TrimSpace(output) == "" {
		return false, err
	}
	if err := json.Unmarshal([]byte(output), target); err != nil {
		return false, &framework.PermanentError{Err: fmt.Errorf("decode memory probe %s: %w; output=%s", name, err, output)}
	}
	return true, nil
}

func (s *MemoryIsolation) waitProbe(ctx context.Context, job framework.SbatchJob, name string, target any, timeout time.Duration) error {
	err := framework.WaitForWithJobAlive(ctx, s.runtime, s.slurm, job, name, timeout, time.Second,
		func(ctx context.Context) (bool, error) {
			ready, err := s.readJSON(ctx, name, target)
			if ready || err != nil || (name != "target.ready.json" && name != "target.below.json") {
				return ready, err
			}
			filename := framework.ShellQuote(path.Join(s.workdir, "target.exit"))
			output, err := s.runtime.Jail().Run(ctx, "if test -s "+filename+"; then cat -- "+filename+"; fi")
			if err == nil && strings.TrimSpace(output) != "" {
				return false, &framework.PermanentError{Err: fmt.Errorf("start memory workload before %s: exit=%s", name, strings.TrimSpace(output))}
			}
			return false, err
		})
	return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, job, err)
}

func (s *MemoryIsolation) CleanupAndReset(ctx context.Context) {
	if len(s.jobs) > 0 {
		collectWorkerDiagnostics(ctx, s.runtime, []framework.WorkerInfo{s.worker}, "")
	}
	ctx, cancel := context.WithTimeout(ctx, memoryCleanupTimeout)
	defer cancel()
	var failures []error
	if !s.imageSetupJob.IsZero() {
		if err := s.slurm.CancelJob(ctx, s.imageSetupJob.ID, time.Minute); err != nil {
			failures = append(failures, err)
		}
	}
	for _, job := range s.jobs {
		if err := s.slurm.CancelJob(ctx, job.ID, time.Minute); err != nil {
			failures = append(failures, err)
		}
	}
	if s.workdir != "" && s.worker.Name != "" {
		stop := "if test -d " + framework.ShellQuote(s.workdir) + "; then touch -- " + framework.ShellQuote(path.Join(s.workdir, "observer.stop")) + " " + framework.ShellQuote(path.Join(s.workdir, "target.release")) + "; fi"
		if _, err := s.runtime.Jail().Run(ctx, stop); err != nil {
			failures = append(failures, err)
		}
		if err := s.runtime.WaitFor(ctx, "memory observer stopped", time.Minute, time.Second,
			func(ctx context.Context) (bool, error) {
				var observation memoryObservation
				found, err := s.readJSON(ctx, "observer.json", &observation)
				return !found || observation.Finished, err
			}); err != nil {
			failures = append(failures, err)
		}
		if s.containerLabel != "" {
			// A unique label also finds containers whose sbatch response was lost.
			command := "docker ps -a --filter " + framework.ShellQuote("label=soperator.e2e.memory="+s.containerLabel) + " --format '{{.ID}}'"
			output, err := s.runtime.Worker(s.worker).Run(ctx, command)
			if err != nil {
				failures = append(failures, err)
			} else {
				for id := range strings.FieldsSeq(output) {
					if _, err := s.runtime.Worker(s.worker).Run(ctx, "docker rm -f "+framework.ShellQuote(id)); err != nil {
						failures = append(failures, err)
					}
				}
			}
		}
		if len(s.jobs) > 0 {
			if err := framework.RunTargetedWorkerSmoke(ctx, s.runtime, s.worker); err != nil {
				failures = append(failures, err)
			}
		}
	}
	cleanupErr := errors.Join(failures...)
	framework.RecordWorkerCleanupError(ctx, cleanupErr)
	if s.ownsReservation {
		cleanupErr = errors.Join(cleanupErr, framework.FinishWorkerReservation(ctx, s.runtime))
	}
	if s.report != nil {
		s.report["jobs"] = s.jobs
		s.report["cleanup_error"] = fmt.Sprint(cleanupErr)
		data, err := json.MarshalIndent(s.report, "", "  ")
		if err == nil && s.artifacts.RunnerDir != "" {
			err = s.artifacts.WriteRunnerFile("memory-isolation.json", string(data)+"\n")
		}
		if err != nil {
			s.runtime.Logf("Write memory isolation evidence: %v", err)
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	info, runtime, slurm, kubectl, selector := s.info, s.runtime, s.slurm, s.kubectl, s.selector
	*s = MemoryIsolation{info: info, runtime: runtime, slurm: slurm, kubectl: kubectl, selector: selector}
	if cleanupErr != nil {
		// The step-family interface has no error return; its wrapper preserves cleanup panics as failures.
		panic(cleanupErr)
	}
}

func validateMemoryLimit(state memorySnapshot) error {
	if state.Cgroup == "" || state.Maximum != strconv.FormatInt(memoryJobLimit, 10) || state.Current < 0 {
		return fmt.Errorf("verify 1GiB job memory limit: %+v", state)
	}
	for _, name := range []string{"oom", "oom_kill", "max"} {
		if value, ok := state.Events[name]; !ok || value < 0 {
			return fmt.Errorf("read memory.events %s from %s: %v", name, state.Cgroup, state.Events)
		}
	}
	if state.Swap {
		for _, value := range state.SwapLimits {
			if value == "0" {
				return nil
			}
		}
		return fmt.Errorf("require swap disabled for bounded RAM exhaustion in %s: %v", state.Cgroup, state.SwapLimits)
	}
	return nil
}

func validateMemoryBelow(workload, ready memoryWorkload, state memorySnapshot) error {
	if err := validateMemoryLimit(state); err != nil {
		return err
	}
	if workload.PID <= 0 || workload.PID != ready.PID || workload.StartTicks == "" || workload.StartTicks != ready.StartTicks ||
		workload.Allocated != memoryJobLimit/4 || workload.HeldSeconds < 3 || state.Current < workload.Allocated || state.Current > memoryJobLimit ||
		state.Events["oom"] != 0 || state.Events["oom_kill"] != 0 {
		return fmt.Errorf("verify allocation below job memory limit: workload=%+v ready=%+v memory=%+v", workload, ready, state)
	}
	return nil
}

func validateMemoryOOM(observation memoryObservation, cgroup string) error {
	if err := validateMemoryLimit(observation.Before); err != nil {
		return err
	}
	if err := validateMemoryLimit(observation.After); err != nil {
		return err
	}
	if !observation.Finished || observation.TimedOut || observation.Error != "" || observation.Before.Cgroup != cgroup || observation.After.Cgroup != cgroup ||
		observation.After.Events["oom"] <= observation.Before.Events["oom"] || observation.After.Events["oom_kill"] <= observation.Before.Events["oom_kill"] {
		return fmt.Errorf("verify job-scoped OOM counters: %+v", observation)
	}
	return nil
}

func validateMemoryNeighbor(before, after memoryWorkload, challenge string, initial, current memorySnapshot) error {
	if err := validateMemoryLimit(current); err != nil {
		return err
	}
	if before.PID <= 0 || before.StartTicks == "" || before.Checksum == "" || after.PID != before.PID || after.StartTicks != before.StartTicks ||
		after.Checksum != before.Checksum || after.Challenge != challenge || challenge == "" || after.Allocated != memoryNeighborHeld ||
		current.Cgroup != initial.Cgroup || current.Current < memoryNeighborHeld ||
		current.Events["oom"] != initial.Events["oom"] || current.Events["oom_kill"] != initial.Events["oom_kill"] {
		return fmt.Errorf("verify surviving neighboring job: before=%+v after=%+v memory_before=%+v memory_after=%+v", before, after, initial, current)
	}
	return nil
}

func validateMemoryHostBaseline(host memoryHost) error {
	if host.BootID == "" || host.Kernel == "" || host.FirstSequence == nil || host.LastSequence < *host.FirstSequence || host.Available < 3*memoryJobLimit {
		return fmt.Errorf("require host OOM evidence and 3GiB available RAM: %+v", host)
	}
	for _, command := range []string{"kubelet", "slurmd"} {
		found := false
		for _, process := range host.Processes {
			found = found || process.Command == command && process.StartTicks != ""
		}
		if !found {
			return fmt.Errorf("find host service %s before memory exhaustion", command)
		}
	}
	return nil
}

func validateMemoryHostIdentity(before, after memoryHost) error {
	if before.BootID == "" || after.BootID != before.BootID || after.Kernel != before.Kernel || after.LastSequence < before.LastSequence ||
		after.FirstSequence == nil || *after.FirstSequence > before.LastSequence+1 {
		return fmt.Errorf("verify host identity and complete kernel log: before=%+v after=%+v", before, after)
	}
	for pid, process := range before.Processes {
		if after.Processes[pid] != process {
			return fmt.Errorf("verify host service process %s (%s) did not restart", pid, process.Command)
		}
	}
	return nil
}

func validateMemoryHost(before, after memoryHost, cgroup string) error {
	if err := validateMemoryHostIdentity(before, after); err != nil {
		return err
	}
	root := strings.TrimPrefix(cgroup, "/sys/fs/cgroup")
	oom, killed := false, false
	for _, record := range after.OOMMessages {
		if record.Sequence <= before.LastSequence {
			return fmt.Errorf("verify fresh kernel OOM evidence: %+v", record)
		}
		message := record.Message
		if strings.Contains(message, "oom-kill:") {
			var group string
			for _, field := range strings.Split(message, ",") {
				if value, ok := strings.CutPrefix(strings.TrimSpace(field), "oom_memcg="); ok {
					group = value
				}
			}
			if group == "" || (group != root && !strings.HasPrefix(group, root+"/")) || !strings.Contains(message, "CONSTRAINT_MEMCG") {
				return fmt.Errorf("detect OOM outside memory job %s: %s", root, message)
			}
			oom = true
		}
		if strings.Contains(strings.ToLower(message), "killed process") {
			if !oom || !strings.Contains(strings.ToLower(message), "memory cgroup out of memory") {
				return fmt.Errorf("attribute kernel OOM victim to memory job %s: %s", root, message)
			}
			killed = true
		}
	}
	if !oom || !killed {
		return fmt.Errorf("find job-scoped kernel OOM and victim evidence for %s: %+v", root, after.OOMMessages)
	}
	return nil
}

func validateMemoryHealth(before, after memoryHealth) error {
	if before.Pod.UID == "" || before.Node.UID == "" || before.Pod.UID != after.Pod.UID || before.Node.UID != after.Node.UID ||
		before.Pod.Spec.NodeName != after.Pod.Spec.NodeName || after.Pod.DeletionTimestamp != nil || after.Node.DeletionTimestamp != nil ||
		after.Pod.Status.Phase != corev1.PodRunning || !kubeobjects.PodReady(after.Pod) || !k8sNodeReady(after.Node) {
		return fmt.Errorf("verify worker pod and host remained ready without replacement")
	}
	for _, condition := range after.Node.Status.Conditions {
		if condition.Type == corev1.NodeMemoryPressure && condition.Status != corev1.ConditionFalse {
			return fmt.Errorf("verify host memory pressure: %+v", condition)
		}
	}
	for _, statuses := range [][2][]corev1.ContainerStatus{
		{before.Pod.Status.ContainerStatuses, after.Pod.Status.ContainerStatuses},
		{before.Pod.Status.InitContainerStatuses, after.Pod.Status.InitContainerStatuses},
	} {
		if len(statuses[0]) != len(statuses[1]) {
			return fmt.Errorf("verify worker container membership did not change")
		}
		for i, initial := range statuses[0] {
			current := statuses[1][i]
			if initial.Name != current.Name || initial.ContainerID != current.ContainerID || initial.RestartCount != current.RestartCount ||
				(initial.Ready && !current.Ready) || (initial.State.Running != nil && current.State.Running == nil) ||
				!reflect.DeepEqual(initial.LastTerminationState, current.LastTerminationState) {
				return fmt.Errorf("verify worker container %s did not restart or terminate: before=%+v after=%+v", initial.Name, initial, current)
			}
		}
	}
	return nil
}
