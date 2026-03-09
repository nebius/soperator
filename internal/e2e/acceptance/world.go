package acceptance

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math/rand"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var (
	instanceIDPattern = regexp.MustCompile(`InstanceId=([^\s]+)`)
	reasonPattern     = regexp.MustCompile(`Reason=([^\n]+)`)
)

type world struct {
	cfg              Config
	commandTimeout   time.Duration
	pollInterval     time.Duration
	replacementDelay time.Duration
	logPrefix        string

	cluster     clusterState
	internalSSH internalSSHConfig
}

type clusterState struct {
	Workers []WorkerRef
}

type WorkerRef struct {
	Name string
}

type internalSSHConfig struct {
	UserName string
}

func (w *world) theProvisionedSlurmClusterIsReachable(ctx context.Context) error {
	if _, err := w.run(ctx, "kubectl", "get", "pods", "-n", "soperator"); err != nil {
		return err
	}
	if _, err := w.run(ctx, "kubectl", "get", "pod", "-n", "soperator", "login-0"); err != nil {
		return err
	}
	if _, err := w.run(ctx, "kubectl", "get", "pod", "-n", "soperator", "controller-0"); err != nil {
		return err
	}

	workerOutput, err := w.execController(ctx, `sinfo -hN -o '%N'`)
	if err != nil {
		return fmt.Errorf("discover worker nodes: %w", err)
	}

	var workers []WorkerRef
	for _, line := range strings.Split(workerOutput, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		workers = append(workers, WorkerRef{Name: name})
	}
	if len(workers) == 0 {
		return fmt.Errorf("no worker nodes discovered")
	}
	w.cluster.Workers = workers

	for _, worker := range w.cluster.Workers {
		if _, err := w.execController(ctx, fmt.Sprintf("scontrol show node %s", shellQuote(worker.Name))); err != nil {
			return fmt.Errorf("read slurm worker state for %s: %w", worker.Name, err)
		}
	}

	w.logf("discovered workers: %s", workerNames(w.cluster.Workers))
	return nil
}

func (w *world) aRegularUserCanSSHFromTheLoginNodeToAWorkerWithoutExtraSSHOptions(ctx context.Context) error {
	worker, err := w.anyWorker()
	if err != nil {
		return err
	}

	userName := w.internalSSH.UserName
	if userName == "" {
		userName = "bob"
	}
	if _, err := w.execJail(ctx, fmt.Sprintf("id %s >/dev/null 2>&1 || printf '\\n' | createuser --without-external-ssh %s", shellQuote(userName), shellQuote(userName))); err != nil {
		return fmt.Errorf("create user %s: %w", userName, err)
	}

	cmd := fmt.Sprintf("su - %s -c 'timeout 30 ssh %s hostname </dev/null'", shellQuote(userName), shellQuote(worker.Name))
	out, err := w.execJail(ctx, cmd)
	if err != nil {
		return fmt.Errorf("ssh from login to worker as %s: %w", userName, err)
	}

	if !strings.Contains(out, worker.Name) {
		return fmt.Errorf("unexpected ssh output %q, expected hostname %q", strings.TrimSpace(out), worker.Name)
	}

	return nil
}

func (w *world) packagesCanBeInstalledOnTheWorkerWithoutBreakingTheNVIDIADriver(ctx context.Context) error {
	worker, err := w.anyWorker()
	if err != nil {
		return err
	}

	steps := []string{
		fmt.Sprintf("ssh %s 'nvidia-smi >/dev/null'", shellQuote(worker.Name)),
		fmt.Sprintf("ssh %s 'DEBIAN_FRONTEND=noninteractive apt-get update'", shellQuote(worker.Name)),
		fmt.Sprintf("ssh %s 'DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends nvitop'", shellQuote(worker.Name)),
		fmt.Sprintf("ssh %s 'nvidia-smi >/dev/null'", shellQuote(worker.Name)),
		fmt.Sprintf("ssh %s 'nvitop --help >/dev/null'", shellQuote(worker.Name)),
	}

	for _, step := range steps {
		if _, err := w.execJail(ctx, step); err != nil {
			return fmt.Errorf("package installation step failed (%s): %w", step, err)
		}
	}

	return nil
}

func (w *world) aMaintenanceEventReplacesTheWorkerNodeAndReturnsItToService(ctx context.Context) error {
	worker, err := w.anyWorker()
	if err != nil {
		return err
	}
	workerName := worker.Name

	nodeState, err := w.execController(ctx, fmt.Sprintf("scontrol show node %s", shellQuote(workerName)))
	if err != nil {
		return fmt.Errorf("read original node state: %w", err)
	}

	originalInstanceID := parseInstanceID(nodeState)
	if originalInstanceID == "" {
		return fmt.Errorf("unable to parse InstanceId from %q", nodeState)
	}

	maintenanceJobID, err := w.execJail(ctx, fmt.Sprintf("sbatch --parsable -w %s --job-name=e2e-node-replacement --wrap=%s", shellQuote(workerName), shellQuote("sleep 600")))
	if err != nil {
		return fmt.Errorf("submit maintenance job: %w", err)
	}
	maintenanceJobID = strings.TrimSpace(maintenanceJobID)
	if maintenanceJobID == "" {
		return fmt.Errorf("empty maintenance job id")
	}
	defer func() {
		if err := w.cancelJob(context.Background(), maintenanceJobID); err != nil {
			w.logf("cleanup: cancel maintenance job: %v", err)
		}
	}()

	if err := w.waitFor(ctx, "maintenance job running", w.replacementDelay, func(waitCtx context.Context) (bool, error) {
		status, err := w.execController(waitCtx, fmt.Sprintf("squeue -h -j %s -o '%%T'", shellQuote(maintenanceJobID)))
		if err != nil {
			return false, err
		}
		return strings.Contains(status, "RUNNING"), nil
	}); err != nil {
		return err
	}

	patch := fmt.Sprintf(`{"status":{"conditions":[{"type":"NebiusMaintenanceScheduled","status":"True","reason":"AcceptanceTest","message":"Maintenance scheduled for node","lastTransitionTime":"%s"}]}}`, time.Now().UTC().Format(time.RFC3339))
	if _, err := w.run(ctx, "kubectl", "patch", "node", originalInstanceID, "--subresource=status", "--type=strategic", "-p", patch); err != nil {
		return fmt.Errorf("patch maintenance condition: %w", err)
	}

	if err := w.waitFor(ctx, "node drain reason", w.replacementDelay, func(waitCtx context.Context) (bool, error) {
		state, err := w.execController(waitCtx, fmt.Sprintf("scontrol show node %s", shellQuote(workerName)))
		if err != nil {
			return false, err
		}

		reason := parseReason(state)
		return strings.Contains(state, "DRAIN") && strings.HasPrefix(reason, "[compute_maintenance]"), nil
	}); err != nil {
		return err
	}

	if err := w.cancelJob(ctx, maintenanceJobID); err != nil {
		return err
	}
	maintenanceJobID = ""

	if err := w.waitFor(ctx, "old instance removal", w.replacementDelay, func(waitCtx context.Context) (bool, error) {
		_, err := w.run(waitCtx, "nebius", "compute", "instance", "get", "--id", originalInstanceID, "--format", "json")
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}); err != nil {
		return err
	}

	if err := w.waitFor(ctx, "replacement node ready", w.replacementDelay, func(waitCtx context.Context) (bool, error) {
		state, err := w.execController(waitCtx, fmt.Sprintf("scontrol show node %s", shellQuote(workerName)))
		if err != nil {
			return false, err
		}

		newInstanceID := parseInstanceID(state)
		if newInstanceID == "" || newInstanceID == originalInstanceID || strings.Contains(state, "DRAIN") {
			return false, nil
		}

		return true, nil
	}); err != nil {
		return err
	}

	if _, err := w.execController(ctx, fmt.Sprintf("srun -w %s nvidia-smi -L >/dev/null", shellQuote(workerName))); err != nil {
		return fmt.Errorf("validate replacement worker is operational: %w", err)
	}

	return nil
}

func (w *world) anyWorker() (WorkerRef, error) {
	if len(w.cluster.Workers) == 0 {
		return WorkerRef{}, fmt.Errorf("no workers discovered")
	}
	return w.cluster.Workers[rand.Intn(len(w.cluster.Workers))], nil
}

func (w *world) execController(ctx context.Context, command string) (string, error) {
	return w.run(ctx, "kubectl", "exec", "-n", "soperator", "controller-0", "--", "bash", "-lc", command)
}

func (w *world) execJail(ctx context.Context, command string) (string, error) {
	return w.run(ctx, "kubectl", "exec", "-n", "soperator", "login-0", "--", "chroot", "/mnt/jail", "bash", "-lc", command)
}

func (w *world) run(ctx context.Context, name string, args ...string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, w.commandTimeout)
	defer cancel()

	w.logf("run: %s %s", name, strings.Join(args, " "))

	cmd := exec.CommandContext(cmdCtx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := stdout.String()
	errOut := strings.TrimSpace(stderr.String())
	if err != nil {
		if errOut != "" {
			return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, errOut)
		}
		return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}

	if errOut != "" {
		log.Printf("%s: stderr: %s", w.logPrefix, errOut)
	}

	return out, nil
}

func (w *world) waitFor(ctx context.Context, description string, timeout time.Duration, condition func(context.Context) (bool, error)) error {
	deadline := time.Now().Add(timeout)
	for {
		done, err := condition(ctx)
		if err == nil && done {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("wait for %s: %w", description, err)
			}
			return fmt.Errorf("wait for %s: timed out after %s", description, timeout)
		}
		if err != nil {
			w.logf("wait for %s still pending: %v", description, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(w.pollInterval):
		}
	}
}

func (w *world) cancelJob(ctx context.Context, maintenanceJobID string) error {
	if maintenanceJobID == "" {
		return nil
	}

	if _, err := w.execController(ctx, fmt.Sprintf("scancel %s || true", shellQuote(maintenanceJobID))); err != nil {
		return fmt.Errorf("cancel maintenance job %s: %w", maintenanceJobID, err)
	}
	return nil
}

func parseInstanceID(state string) string {
	match := instanceIDPattern.FindStringSubmatch(state)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func parseReason(state string) string {
	match := reasonPattern.FindStringSubmatch(state)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func workerNames(workers []WorkerRef) string {
	names := make([]string, 0, len(workers))
	for _, worker := range workers {
		names = append(names, worker.Name)
	}
	return strings.Join(names, ", ")
}
