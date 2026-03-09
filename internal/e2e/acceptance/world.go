package acceptance

import (
	"bytes"
	"context"
	"fmt"
	"log"
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

	workerName         string
	maintenanceJobID   string
	originalInstanceID string
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

	worker, err := w.execController(ctx, `sinfo -hN -o '%N' | head -n1`)
	if err != nil {
		return fmt.Errorf("discover worker node: %w", err)
	}
	w.workerName = strings.TrimSpace(worker)
	if w.workerName == "" {
		return fmt.Errorf("no worker node discovered")
	}

	if _, err := w.execController(ctx, fmt.Sprintf("scontrol show node %s", shellQuote(w.workerName))); err != nil {
		return fmt.Errorf("read slurm worker state: %w", err)
	}

	w.logf("selected worker %s", w.workerName)
	return nil
}

func (w *world) aRegularUserCanSSHFromTheLoginNodeToAWorkerWithoutExtraSSHOptions(ctx context.Context) error {
	userName := "bob"
	if _, err := w.execJail(ctx, fmt.Sprintf("id %s >/dev/null 2>&1 || printf '\\n' | createuser --without-external-ssh %s", shellQuote(userName), shellQuote(userName))); err != nil {
		return fmt.Errorf("create user %s: %w", userName, err)
	}

	cmd := fmt.Sprintf("su - %s -c 'timeout 30 ssh %s hostname </dev/null'", shellQuote(userName), shellQuote(w.workerName))
	out, err := w.execJail(ctx, cmd)
	if err != nil {
		return fmt.Errorf("ssh from login to worker as %s: %w", userName, err)
	}

	if !strings.Contains(out, w.workerName) {
		return fmt.Errorf("unexpected ssh output %q, expected hostname %q", strings.TrimSpace(out), w.workerName)
	}

	return nil
}

func (w *world) packagesCanBeInstalledOnTheWorkerWithoutBreakingTheNVIDIADriver(ctx context.Context) error {
	steps := []string{
		fmt.Sprintf("ssh %s 'nvidia-smi >/dev/null'", shellQuote(w.workerName)),
		fmt.Sprintf("ssh %s 'DEBIAN_FRONTEND=noninteractive apt-get update'", shellQuote(w.workerName)),
		fmt.Sprintf("ssh %s 'DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends nvitop'", shellQuote(w.workerName)),
		fmt.Sprintf("ssh %s 'nvidia-smi >/dev/null'", shellQuote(w.workerName)),
		fmt.Sprintf("ssh %s 'nvitop --help >/dev/null'", shellQuote(w.workerName)),
	}

	for _, step := range steps {
		if _, err := w.execJail(ctx, step); err != nil {
			return fmt.Errorf("package installation step failed (%s): %w", step, err)
		}
	}

	return nil
}

func (w *world) aMaintenanceEventReplacesTheWorkerNodeAndReturnsItToService(ctx context.Context) error {
	nodeState, err := w.execController(ctx, fmt.Sprintf("scontrol show node %s", shellQuote(w.workerName)))
	if err != nil {
		return fmt.Errorf("read original node state: %w", err)
	}

	w.originalInstanceID = parseInstanceID(nodeState)
	if w.originalInstanceID == "" {
		return fmt.Errorf("unable to parse InstanceId from %q", nodeState)
	}

	jobID, err := w.execJail(ctx, fmt.Sprintf("sbatch --parsable -w %s --job-name=e2e-node-replacement --wrap=%s", shellQuote(w.workerName), shellQuote("sleep 600")))
	if err != nil {
		return fmt.Errorf("submit maintenance job: %w", err)
	}
	w.maintenanceJobID = strings.TrimSpace(jobID)
	if w.maintenanceJobID == "" {
		return fmt.Errorf("empty maintenance job id")
	}
	defer func() {
		if err := w.cancelJob(context.Background()); err != nil {
			w.logf("cleanup: cancel maintenance job: %v", err)
		}
	}()

	if err := w.waitFor(ctx, "maintenance job running", w.replacementDelay, func(waitCtx context.Context) (bool, error) {
		status, err := w.execController(waitCtx, fmt.Sprintf("squeue -h -j %s -o '%%T'", shellQuote(w.maintenanceJobID)))
		if err != nil {
			return false, err
		}
		return strings.Contains(status, "RUNNING"), nil
	}); err != nil {
		return err
	}

	patch := fmt.Sprintf(`{"status":{"conditions":[{"type":"NebiusMaintenanceScheduled","status":"True","reason":"AcceptanceTest","message":"Maintenance scheduled for node","lastTransitionTime":"%s"}]}}`, time.Now().UTC().Format(time.RFC3339))
	if _, err := w.run(ctx, "kubectl", "patch", "node", w.originalInstanceID, "--subresource=status", "--type=strategic", "-p", patch); err != nil {
		return fmt.Errorf("patch maintenance condition: %w", err)
	}

	if err := w.waitFor(ctx, "node drain reason", w.replacementDelay, func(waitCtx context.Context) (bool, error) {
		state, err := w.execController(waitCtx, fmt.Sprintf("scontrol show node %s", shellQuote(w.workerName)))
		if err != nil {
			return false, err
		}

		reason := parseReason(state)
		return strings.Contains(state, "DRAIN") && strings.HasPrefix(reason, "[compute_maintenance]"), nil
	}); err != nil {
		return err
	}

	if err := w.cancelJob(ctx); err != nil {
		return err
	}

	if err := w.waitFor(ctx, "old instance removal", w.replacementDelay, func(waitCtx context.Context) (bool, error) {
		_, err := w.run(waitCtx, "nebius", "compute", "instance", "get", "--id", w.originalInstanceID, "--format", "json")
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
		state, err := w.execController(waitCtx, fmt.Sprintf("scontrol show node %s", shellQuote(w.workerName)))
		if err != nil {
			return false, err
		}

		newInstanceID := parseInstanceID(state)
		if newInstanceID == "" || newInstanceID == w.originalInstanceID || strings.Contains(state, "DRAIN") {
			return false, nil
		}

		return true, nil
	}); err != nil {
		return err
	}

	if _, err := w.execController(ctx, fmt.Sprintf("srun -w %s nvidia-smi -L >/dev/null", shellQuote(w.workerName))); err != nil {
		return fmt.Errorf("validate replacement worker is operational: %w", err)
	}

	return nil
}

func (w *world) theWorkflowDestroyStepRemovesTheE2ECluster(ctx context.Context) error {
	out, err := w.run(ctx,
		"nebius", "mk8s", "cluster", "list",
		"--parent-id", w.cfg.NebiusProjectID,
		"--format", "json",
	)
	if err != nil {
		return fmt.Errorf("list mk8s clusters: %w", err)
	}

	if strings.Contains(out, w.cfg.ClusterName) {
		return fmt.Errorf("cluster %s still exists", w.cfg.ClusterName)
	}

	return nil
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

func (w *world) cancelJob(ctx context.Context) error {
	if w.maintenanceJobID == "" {
		return nil
	}

	if _, err := w.execController(ctx, fmt.Sprintf("scancel %s || true", shellQuote(w.maintenanceJobID))); err != nil {
		return fmt.Errorf("cancel maintenance job %s: %w", w.maintenanceJobID, err)
	}
	w.maintenanceJobID = ""
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
