//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const (
	nodeReplacementJobTimeout    = 25 * time.Minute
	nodeReplacementDrainTimeout  = 25 * time.Minute
	nodeReplacementRemoveTimeout = 25 * time.Minute
	nodeReplacementReadyTimeout  = 25 * time.Minute
)

var (
	instanceIDPattern = regexp.MustCompile(`InstanceId=([^\s]+)`)
	reasonPattern     = regexp.MustCompile(`Reason=([^\n]+)`)
)

type nodeReplacementScenario struct {
	targetWorker     framework.WorkerRef
	originalInstance string
	maintenanceJobID string
}

var _ = Describe("Node replacement", func() {
	It("replaces the selected worker after a maintenance event", func(ctx SpecContext) {
		state := nodeReplacementScenario{}

		By("selecting a worker for the maintenance test")
		worker, err := suite.AnyWorker()
		Expect(err).NotTo(HaveOccurred())
		state.targetWorker = worker

		By("capturing the worker's current instance id")
		nodeState, err := suite.ExecController(ctx, fmt.Sprintf("scontrol show node %s", framework.ShellQuote(state.targetWorker.Name)))
		Expect(err).NotTo(HaveOccurred())

		state.originalInstance = parseInstanceID(nodeState)
		Expect(state.originalInstance).NotTo(BeEmpty())

		By("submitting a test job pinned to the selected worker")
		jobID, err := suite.ExecJail(ctx, fmt.Sprintf(
			"sbatch --parsable -w %s --job-name=e2e-node-replacement --wrap=%s",
			framework.ShellQuote(state.targetWorker.Name),
			framework.ShellQuote("sleep 600"),
		))
		Expect(err).NotTo(HaveOccurred())

		state.maintenanceJobID = strings.TrimSpace(jobID)
		Expect(state.maintenanceJobID).NotTo(BeEmpty())

		DeferCleanup(func() {
			if state.maintenanceJobID == "" {
				return
			}

			if _, cleanupErr := suite.ExecController(context.Background(), fmt.Sprintf("scancel %s || true", framework.ShellQuote(state.maintenanceJobID))); cleanupErr != nil {
				suite.Logf("cleanup: cancel maintenance job: %v", cleanupErr)
			}
		})

		By("waiting for the test job to enter RUNNING state")
		Eventually(func(ctx context.Context) (bool, error) {
			status, runErr := suite.ExecController(ctx, fmt.Sprintf("squeue -h -j %s -o '%%T'", framework.ShellQuote(state.maintenanceJobID)))
			if runErr != nil {
				return false, runErr
			}

			return strings.Contains(status, "RUNNING"), nil
		}, nodeReplacementJobTimeout, 10*time.Second).WithContext(ctx).Should(BeTrue())

		By("triggering the maintenance condition on the original instance")
		patch := fmt.Sprintf(
			`{"status":{"conditions":[{"type":"NebiusMaintenanceScheduled","status":"True","reason":"AcceptanceTest","message":"Maintenance scheduled for node","lastTransitionTime":"%s"}]}}`,
			time.Now().UTC().Format(time.RFC3339),
		)
		_, err = suite.Run(
			ctx,
			"kubectl", "patch", "node", state.originalInstance,
			"--subresource=status", "--type=strategic", "-p", patch,
		)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the worker to drain with the maintenance reason")
		Eventually(func(ctx context.Context) (bool, error) {
			nodeState, runErr := suite.ExecController(ctx, fmt.Sprintf("scontrol show node %s", framework.ShellQuote(state.targetWorker.Name)))
			if runErr != nil {
				return false, runErr
			}

			reason := parseReason(nodeState)
			return strings.Contains(nodeState, "DRAIN") && strings.HasPrefix(reason, "[compute_maintenance]"), nil
		}, nodeReplacementDrainTimeout, 15*time.Second).WithContext(ctx).Should(BeTrue())

		By("cancelling the test job")
		_, err = suite.ExecController(ctx, fmt.Sprintf("scancel %s || true", framework.ShellQuote(state.maintenanceJobID)))
		Expect(err).NotTo(HaveOccurred())
		state.maintenanceJobID = ""

		By("waiting for the original instance to be removed")
		Eventually(func(ctx context.Context) bool {
			_, runErr := suite.Run(ctx, "nebius", "compute", "instance", "get", "--id", state.originalInstance, "--format", "json")
			return runErr != nil && strings.Contains(runErr.Error(), "not found")
		}, nodeReplacementRemoveTimeout, 30*time.Second).WithContext(ctx).Should(BeTrue())

		By("waiting for a replacement instance to join the cluster")
		Eventually(func(ctx context.Context) (bool, error) {
			nodeState, runErr := suite.ExecController(ctx, fmt.Sprintf("scontrol show node %s", framework.ShellQuote(state.targetWorker.Name)))
			if runErr != nil {
				return false, runErr
			}

			newInstanceID := parseInstanceID(nodeState)
			if newInstanceID == "" || newInstanceID == state.originalInstance || strings.Contains(nodeState, "DRAIN") {
				return false, nil
			}

			return true, nil
		}, nodeReplacementReadyTimeout, 60*time.Second).WithContext(ctx).Should(BeTrue())

		By("verifying GPU access on the replacement node")
		_, err = suite.ExecJail(ctx, fmt.Sprintf("srun -w %s nvidia-smi -L >/dev/null", framework.ShellQuote(state.targetWorker.Name)))
		if err != nil {
			nodeState, stateErr := suite.ExecController(ctx, fmt.Sprintf("scontrol show node %s", framework.ShellQuote(state.targetWorker.Name)))
			if stateErr == nil {
				suite.Logf("replacement worker state after failed final validation:\n%s", strings.TrimSpace(nodeState))
			}
		}
		Expect(err).NotTo(HaveOccurred())
	})
})

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
