package sharedsteps

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	nvidiaLibrariesMinWorkers = 2
	// Container jobs pull the small Ubuntu image cold on a fresh cluster.
	nvidiaLibrariesJobTimeout            = 5 * time.Minute
	nvidiaLibrariesCleanupTimeout        = 2 * time.Minute
	nvidiaLibrariesJobNamePrefix         = "e2e-nvidia-libraries-"
	nvidiaLibrariesDockerContainerPrefix = "e2e-nvidia-libraries-"

	nvidiaPodLibraryKey   = "NVIDIA_POD_LIBRARY"
	nvidiaLibraryViewKey  = "NVIDIA_LIBRARY_VIEW"
	nvidiaLibraryKindFile = "file"
)

// nvidiaLibrariesRequiredCapabilities must all be configured on the slurmd
// container of a GPU worker, so the pod receives every driver library the jail
// and job containers are expected to see.
var nvidiaLibrariesRequiredCapabilities = []string{"compute", "graphics", "utility", "video", "ngx"}

// nvidiaDriverLibrary is a driver library nvidia-container-cli lists in a worker pod.
type nvidiaDriverLibrary struct {
	path string
	size int64
}

func (l nvidiaDriverLibrary) name() string {
	return path.Base(l.path)
}

// nvidiaLibraryView is what a job task on one node sees for a listed library.
type nvidiaLibraryView struct {
	node string
	name string
	path string
	// kind is file, symlink, other or missing.
	kind string
	size int64
	// sonames are the linker cache entries resolving to the file.
	sonames []string
}

type NVIDIALibraries struct {
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector

	workers  []framework.WorkerInfo
	pods     []framework.WorkerPodInfo
	listings map[string][]nvidiaDriverLibrary
	// libraries is the listing the selected workers share.
	libraries []nvidiaDriverLibrary
	job       framework.SbatchJob
	// dockerContainerPrefix names the job's containers on the workers for cleanup.
	dockerContainerPrefix string
	views                 []nvidiaLibraryView
}

func NewNVIDIALibraries(
	runtime framework.Runtime,
	slurm *framework.SlurmClient,
	kubectl *framework.KubectlClient,
	selector *framework.WorkerSelector,
) *NVIDIALibraries {
	return &NVIDIALibraries{runtime: runtime, slurm: slurm, kubectl: kubectl, selector: selector}
}

func (s *NVIDIALibraries) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^healthy GPU workers are selected from every GPU NodeSet for NVIDIA library checks$`, s.selectGPUWorkers)
	sc.Step(`^healthy Docker-enabled GPU workers are selected from every Docker-enabled GPU NodeSet for NVIDIA library checks$`, s.selectDockerGPUWorkers)
	sc.Step(`^their slurmd containers are configured with the required NVIDIA driver capabilities$`, s.slurmdContainersAreConfiguredWithRequiredCapabilities)
	sc.Step(`^the NVIDIA driver libraries mounted into the worker pods are listed$`, s.listPodLibraries)
	sc.Step(`^every worker pod lists the same NVIDIA driver libraries$`, s.podsListTheSameLibraries)
	sc.Step(`^an? (native|Enroot|Docker) job on the selected workers reports its view of the listed libraries$`, s.jobReportsItsViewOfTheListedLibraries)
	sc.Step(`^the job sees every listed library as the pod's file with a soname in the linker cache$`, s.jobSeesEveryListedLibrary)
}

func (s *NVIDIALibraries) CleanupAndReset(ctx context.Context) {
	if !s.job.IsZero() {
		runCleanupPhase(ctx, s.runtime, "stop NVIDIA libraries job "+s.job.ID, 2*nvidiaLibrariesCleanupTimeout, func(ctx context.Context) error {
			// A job or container left running keeps the selected workers busy for the next scenario.
			if err := s.stopJob(ctx); err != nil {
				framework.RecordWorkerCleanupError(ctx, err)
				return err
			}
			return nil
		})
	}
	s.workers = nil
	s.pods = nil
	s.listings = nil
	s.libraries = nil
	s.job = framework.SbatchJob{}
	s.dockerContainerPrefix = ""
	s.views = nil
}

// stopJob cancels the job, removes its Docker containers, which the cancellation does not reach
// since it only kills the Docker client, and waits for the job to leave the queue.
func (s *NVIDIALibraries) stopJob(ctx context.Context) error {
	if err := s.slurm.CancelJob(ctx, s.job.ID, 0); err != nil {
		return err
	}
	if err := s.removeDockerContainers(ctx); err != nil {
		return err
	}
	return s.slurm.WaitForJobGone(ctx, s.job.ID, nvidiaLibrariesCleanupTimeout)
}

func (s *NVIDIALibraries) removeDockerContainers(ctx context.Context) error {
	if s.dockerContainerPrefix == "" {
		return nil
	}
	var errs []error
	for _, worker := range s.workers {
		scope := s.runtime.Worker(worker)
		out, err := scope.RunWithDefaultRetry(ctx, fmt.Sprintf("sudo docker ps -aq --filter name=%s", framework.ShellQuote(s.dockerContainerPrefix)))
		if err != nil {
			errs = append(errs, fmt.Errorf("list Docker containers %s* on worker %s: %w", s.dockerContainerPrefix, worker.Name, err))
			continue
		}
		for id := range parseIDSet(out) {
			if _, err := scope.Run(ctx, "sudo docker rm -f "+framework.ShellQuote(id)); err != nil {
				errs = append(errs, fmt.Errorf("remove Docker container %s on worker %s: %w", id, worker.Name, err))
			}
		}
	}
	return errors.Join(errs...)
}

func (s *NVIDIALibraries) selectGPUWorkers(ctx context.Context) error {
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	return s.selectWorkers(ctx, snapshot)
}

func (s *NVIDIALibraries) selectDockerGPUWorkers(ctx context.Context) error {
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	return s.selectWorkers(ctx, snapshot.WithDocker())
}

// selectWorkers takes an idle GPU worker from every GPU NodeSet, since the
// linker cache in the shared jail only works with one driver version across
// the cluster, and at least two in total so the job spans nodes.
func (s *NVIDIALibraries) selectWorkers(ctx context.Context, snapshot framework.WorkerSnapshot) error {
	workers, err := pickIdleGPUWorkersFromEveryNodeSet(snapshot, nvidiaLibrariesMinWorkers)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	pods := make([]framework.WorkerPodInfo, 0, len(workers))
	for _, worker := range workers {
		pod, err := s.kubectl.WorkerPodForSlurmNode(ctx, worker.Name)
		if err != nil {
			return err
		}
		pods = append(pods, pod)
	}
	s.workers = workers
	s.pods = pods
	s.runtime.Logf("NVIDIA libraries: selected workers=%s", strings.Join(describeNVIDIALibraryWorkers(workers), ","))
	return nil
}

func (s *NVIDIALibraries) slurmdContainersAreConfiguredWithRequiredCapabilities(ctx context.Context) error {
	if err := s.requireWorkers(); err != nil {
		return err
	}
	var problems []error
	for i, pod := range s.pods {
		worker := s.workers[i].Name
		value, err := s.kubectl.WorkerContainerEnvironmentVariable(ctx, pod, slurmdContainerName, nvidiaDriverCapabilitiesName)
		if err != nil {
			return err
		}
		configured, err := parseNVIDIADriverCapabilities(value)
		if err != nil {
			return fmt.Errorf("read NVIDIA driver capabilities of worker %s: %w", worker, err)
		}
		s.runtime.Logf("NVIDIA libraries: worker %s is configured with %s=%q", worker, nvidiaDriverCapabilitiesName, value)
		if missing := missingNVIDIADriverCapabilities(configured, nvidiaLibrariesRequiredCapabilities); len(missing) > 0 {
			problems = append(problems, fmt.Errorf("check %s=%q on worker %s: missing required capabilities %s",
				nvidiaDriverCapabilitiesName, value, worker, strings.Join(missing, ",")))
		}
	}
	return errors.Join(problems...)
}

func (s *NVIDIALibraries) listPodLibraries(ctx context.Context) error {
	if err := s.requireWorkers(); err != nil {
		return err
	}
	listings := make(map[string][]nvidiaDriverLibrary, len(s.pods))
	for i, pod := range s.pods {
		worker := s.workers[i].Name
		output, err := s.runtime.WorkerPod(pod).RunWithDefaultRetry(ctx, nvidiaPodLibrariesCommand())
		if err != nil {
			return fmt.Errorf("list NVIDIA driver libraries in pod %s of worker %s: %w", pod.PodName, worker, err)
		}
		libraries, err := parseNVIDIAPodLibraries(output)
		if err != nil {
			return fmt.Errorf("parse NVIDIA driver libraries listed in pod %s of worker %s: %w", pod.PodName, worker, err)
		}
		s.runtime.Logf("NVIDIA libraries: pod %s of worker %s lists %d driver libraries", pod.PodName, worker, len(libraries))
		listings[worker] = libraries
	}
	s.listings = listings
	return nil
}

func (s *NVIDIALibraries) podsListTheSameLibraries() error {
	if err := s.requireWorkers(); err != nil {
		return err
	}
	if len(s.listings) != len(s.workers) {
		return fmt.Errorf("list the NVIDIA driver libraries of the worker pods first")
	}
	reference := s.listings[s.workers[0].Name]
	var problems []error
	for _, worker := range s.workers[1:] {
		if diff := describeNVIDIALibraryListingDiff(reference, s.listings[worker.Name]); diff != "" {
			problems = append(problems, fmt.Errorf("compare NVIDIA driver libraries of workers %s and %s: %s",
				describeNVIDIALibraryWorker(s.workers[0]), describeNVIDIALibraryWorker(worker), diff))
		}
	}
	if err := errors.Join(problems...); err != nil {
		return err
	}
	s.libraries = reference
	return nil
}

func (s *NVIDIALibraries) jobReportsItsViewOfTheListedLibraries(ctx context.Context, runtime string) error {
	if err := s.requireWorkers(); err != nil {
		return err
	}
	if len(s.libraries) == 0 {
		return fmt.Errorf("compare the NVIDIA driver libraries of the worker pods first")
	}
	names := make([]string, 0, len(s.libraries))
	for _, library := range s.libraries {
		names = append(names, library.name())
	}
	wrap, err := nvidiaLibrariesJobCommand(runtime, nvidiaLibraryViewProbe(names))
	if err != nil {
		return err
	}
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      nvidiaLibrariesJobNamePrefix + strings.ToLower(runtime),
		Nodes:        len(s.workers),
		Nodelist:     framework.WorkerNames(s.workers),
		GPUsPerNode:  1,
		TasksPerNode: 1,
		Wrap:         wrap,
	})
	if err != nil {
		return err
	}
	s.job = job
	if runtime == "Docker" {
		s.dockerContainerPrefix = nvidiaLibrariesDockerContainerPrefix + job.ID + "-"
	}
	s.runtime.Logf("NVIDIA libraries: submitted %s job id=%s workers=%s stdout=%s",
		runtime, job.ID, strings.Join(framework.WorkerNames(s.workers), ","), job.StdoutPath)
	if err := framework.WaitForJobSucceeded(ctx, s.runtime, s.slurm, job, nvidiaLibrariesJobTimeout); err != nil {
		return err
	}
	output, err := readJobFile(ctx, s.runtime, job.StdoutPath)
	if err != nil {
		return err
	}
	views, err := parseNVIDIALibraryViews(output)
	if err != nil {
		return fmt.Errorf("parse library views from %s job %s: %w", runtime, job.ID, err)
	}
	s.views = views
	return nil
}

func (s *NVIDIALibraries) jobSeesEveryListedLibrary() error {
	if len(s.views) == 0 {
		return fmt.Errorf("run the library view job first")
	}
	return validateNVIDIALibraryViews(framework.WorkerNames(s.workers), s.libraries, s.views)
}

func describeNVIDIALibraryWorker(worker framework.WorkerInfo) string {
	return worker.Name + " (NodeSet " + worker.NodeSetName + ")"
}

func describeNVIDIALibraryWorkers(workers []framework.WorkerInfo) []string {
	described := make([]string, 0, len(workers))
	for _, worker := range workers {
		described = append(described, describeNVIDIALibraryWorker(worker))
	}
	return described
}

func (s *NVIDIALibraries) requireWorkers() error {
	if len(s.workers) == 0 {
		return fmt.Errorf("select GPU workers for NVIDIA library checks first")
	}
	return nil
}

// nvidiaPodLibrariesCommand prints every driver library nvidia-container-cli
// finds in the worker pod with its size, the reference every job view is
// compared against.
func nvidiaPodLibrariesCommand() string {
	return fmt.Sprintf(`set -o pipefail
nvidia-container-cli list --libraries | while IFS= read -r path; do
[ -n "$path" ] || continue
printf '%s=%%s %%s\n' "$(stat -Lc %%s "$path" 2>/dev/null || echo -1)" "$path"
done`, nvidiaPodLibraryKey)
}

func parseNVIDIAPodLibraries(output string) ([]nvidiaDriverLibrary, error) {
	var libraries []nvidiaDriverLibrary
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		value, found := strings.CutPrefix(strings.TrimSpace(scanner.Text()), nvidiaPodLibraryKey+"=")
		if !found {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) != 2 {
			return nil, fmt.Errorf("parse pod library %q: want size and path", value)
		}
		size, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse size in pod library %q: %w", value, err)
		}
		if size <= 0 {
			return nil, fmt.Errorf("stat pod library %s: got size %d", fields[1], size)
		}
		libraries = append(libraries, nvidiaDriverLibrary{path: fields[1], size: size})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan pod library output: %w", err)
	}
	if len(libraries) == 0 {
		return nil, fmt.Errorf("find %s lines in output", nvidiaPodLibraryKey)
	}
	slices.SortFunc(libraries, func(a, b nvidiaDriverLibrary) int { return strings.Compare(a.path, b.path) })
	return libraries, nil
}

// describeNVIDIALibraryListingDiff is empty when both listings carry the same
// paths with the same sizes.
func describeNVIDIALibraryListingDiff(reference, other []nvidiaDriverLibrary) string {
	sizes := func(libraries []nvidiaDriverLibrary) map[string]int64 {
		result := make(map[string]int64, len(libraries))
		for _, library := range libraries {
			result[library.path] = library.size
		}
		return result
	}
	referenceSizes, otherSizes := sizes(reference), sizes(other)
	var onlyReference, onlyOther, sizeMismatch []string
	for _, library := range reference {
		size, found := otherSizes[library.path]
		switch {
		case !found:
			onlyReference = append(onlyReference, library.path)
		case size != library.size:
			sizeMismatch = append(sizeMismatch, fmt.Sprintf("%s (%d vs %d bytes)", library.path, library.size, size))
		}
	}
	for _, library := range other {
		if _, found := referenceSizes[library.path]; !found {
			onlyOther = append(onlyOther, library.path)
		}
	}
	var parts []string
	if len(onlyReference) > 0 {
		parts = append(parts, "only on the first: "+strings.Join(onlyReference, ", "))
	}
	if len(onlyOther) > 0 {
		parts = append(parts, "only on the second: "+strings.Join(onlyOther, ", "))
	}
	if len(sizeMismatch) > 0 {
		parts = append(parts, "size differs: "+strings.Join(sizeMismatch, ", "))
	}
	return strings.Join(parts, "; ")
}

// nvidiaLibrariesJobCommand runs the probe on every node of the allocation
// through the given runtime. Container jobs ask their runtime hook for all
// driver capabilities, so the container sees every library the jail has.
func nvidiaLibrariesJobCommand(runtime, probe string) (string, error) {
	command := framework.BashLC(probe)
	// The batch shell expands SLURM_JOB_ID, so cleanup can find the containers by job ID.
	dockerName := nvidiaLibrariesDockerContainerPrefix + "${SLURM_JOB_ID}-${SLURM_NODEID}"
	switch runtime {
	case "native":
		return "srun " + command, nil
	case "Enroot":
		// Pyxis passes the job environment to the Enroot NVIDIA hook, which
		// injects the driver only when NVIDIA_VISIBLE_DEVICES is set.
		return fmt.Sprintf("%s=all %s=all srun --container-image=%s %s",
			nvidiaVisibleDevicesName, nvidiaDriverCapabilitiesName, framework.ShellQuote(enrootLifecycleImage), command), nil
	case "Docker":
		// --gpus selects the devices; the capabilities go through the environment
		// because Docker's own capabilities option has no "all".
		return fmt.Sprintf("srun docker run --rm --name %s --gpus=all -e SLURMD_NODENAME -e %s=all %s %s",
			dockerName, nvidiaDriverCapabilitiesName, framework.ShellQuote(ubuntuDockerImage), command), nil
	default:
		return "", fmt.Errorf("run NVIDIA libraries job: unsupported runtime %q", runtime)
	}
}

// nvidiaLibraryViewProbe reports, per listed library name, what the task sees
// at the flattened driver library path: the file kind, its size and the linker
// cache entries resolving to it. Only cache entries sharing the library's stem
// are resolved, which keeps the loop cheap in the jail's large cache.
func nvidiaLibraryViewProbe(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, framework.ShellQuote(name))
	}
	return fmt.Sprintf(`libdir="/usr/lib/$(uname -m)-linux-gnu"
node="${SLURMD_NODENAME:-$(hostname)}"
cache="$(ldconfig -p)"
for name in %s; do
file="$libdir/$name"
kind=missing; size=0; sonames=""
if [ -L "$file" ]; then kind=symlink
elif [ -f "$file" ]; then kind=file; size="$(stat -c %%s "$file")"
elif [ -e "$file" ]; then kind=other
fi
if [ "$kind" = file ]; then
real="$(readlink -f "$file")"
stem="${name%%%%.so*}.so"
for entry in $(printf '%%s\n' "$cache" | awk -v stem="$stem" 'index($1, stem) == 1 { print $NF }'); do
if [ "$(readlink -f "$entry")" = "$real" ]; then sonames="$sonames,$(basename "$entry")"; fi
done
fi
sonames="${sonames#,}"
printf '%s=%%s %%s %%s %%s %%s %%s\n' "$node" "$name" "$file" "$kind" "$size" "${sonames:--}"
done`, strings.Join(quoted, " "), nvidiaLibraryViewKey)
}

func parseNVIDIALibraryViews(output string) ([]nvidiaLibraryView, error) {
	var views []nvidiaLibraryView
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		value, found := strings.CutPrefix(strings.TrimSpace(scanner.Text()), nvidiaLibraryViewKey+"=")
		if !found {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) != 6 {
			return nil, fmt.Errorf("parse library view %q: want node, library, path, kind, size and sonames", value)
		}
		size, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse size in library view %q: %w", value, err)
		}
		var sonames []string
		if fields[5] != "-" {
			sonames = strings.Split(fields[5], ",")
		}
		views = append(views, nvidiaLibraryView{
			node:    fields[0],
			name:    fields[1],
			path:    fields[2],
			kind:    fields[3],
			size:    size,
			sonames: sonames,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan library view output: %w", err)
	}
	if len(views) == 0 {
		return nil, fmt.Errorf("find %s lines in output", nvidiaLibraryViewKey)
	}
	return views, nil
}

// validateNVIDIALibraryViews requires every node to see every listed library
// as a regular file of the pod's size with at least one linker cache entry
// resolving to it.
func validateNVIDIALibraryViews(nodes []string, libraries []nvidiaDriverLibrary, views []nvidiaLibraryView) error {
	type key struct{ node, name string }
	byKey := make(map[key]nvidiaLibraryView, len(views))
	for _, view := range views {
		byKey[key{view.node, view.name}] = view
	}
	var problems []error
	for _, node := range nodes {
		for _, library := range libraries {
			name := library.name()
			view, found := byKey[key{node, name}]
			switch {
			case !found:
				problems = append(problems, fmt.Errorf("check %s on %s: no view reported", name, node))
			case view.kind != nvidiaLibraryKindFile:
				problems = append(problems, fmt.Errorf("check %s on %s: want a regular file at %s, got %s", name, node, view.path, view.kind))
			case view.size != library.size:
				problems = append(problems, fmt.Errorf("check %s on %s: want %d bytes as in the worker pod, got %d", name, node, library.size, view.size))
			case len(view.sonames) == 0:
				problems = append(problems, fmt.Errorf("check %s on %s: no linker cache entry resolves to %s", name, node, view.path))
			}
		}
	}
	return errors.Join(problems...)
}
