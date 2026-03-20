//go:build acceptance

package acceptance

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func passiveChecksTest(ctx SpecContext) {
	state := struct {
		workerName string
		cpuJobID   string
		gpuJobID   string
		shmMarker  string
	}{
		shmMarker: fmt.Sprintf("e2e-s131-%d", time.Now().Unix()),
	}

	suite.Given(ctx, "a worker is selected for passive-check validation", func(_ SpecContext) {
		worker, err := suite.AnyWorker()
		Expect(err).NotTo(HaveOccurred())

		state.workerName = worker.Name
		suite.Logf("using worker %s for passive checks", state.workerName)
	})

	suite.Then(ctx, "periodic passive check-runner outputs exist without obvious Python errors", func(ctx SpecContext) {
		path := latestJailFile(ctx, "/opt/soperator-outputs/slurm_scripts/worker-*.check_runner.hc_program.out")
		content := readJailFile(ctx, path)
		Expect(content).NotTo(ContainSubstring("Traceback"))
		Expect(content).NotTo(ContainSubstring("No such file or directory"))
	})

	suite.When(ctx, "a CPU-only job is submitted", func(ctx SpecContext) {
		state.cpuJobID = submitBatchJob(
			ctx,
			"srun true",
			"-N", "1",
			"-w", state.workerName,
			"--job-name", "e2e-s131-cpu",
		)
	})

	DeferCleanup(func() {
		cancelJob(state.cpuJobID)
		cancelJob(state.gpuJobID)
	})

	suite.Then(ctx, "the CPU-only job produces prolog and epilog outputs", func(ctx SpecContext) {
		waitForJobCompletion(ctx, state.cpuJobID, 10*time.Minute)

		for _, suffix := range []string{"prolog", "epilog"} {
			path := latestJailFile(ctx, fmt.Sprintf("/opt/soperator-outputs/slurm_scripts/%s.check_runner.%s.out", state.workerName, suffix))
			content := readJailFile(ctx, path)
			Expect(content).NotTo(ContainSubstring("Traceback"))
		}
	})

	suite.When(ctx, "a GPU job writes into /dev/shm on the selected worker", func(ctx SpecContext) {
		command := fmt.Sprintf("srun bash -lc 'echo hello > /dev/shm/%s; sleep 5'", state.shmMarker)
		state.gpuJobID = submitBatchJob(
			ctx,
			command,
			"-N", "1",
			"-w", state.workerName,
			"--gpus-per-node", "1",
			"--job-name", "e2e-s131-gpu",
		)
	})

	suite.Then(ctx, "the GPU job produces prolog and epilog outputs", func(ctx SpecContext) {
		waitForJobCompletion(ctx, state.gpuJobID, 10*time.Minute)

		for _, suffix := range []string{"prolog", "epilog"} {
			path := latestJailFile(ctx, fmt.Sprintf("/opt/soperator-outputs/slurm_scripts/%s.check_runner.%s.out", state.workerName, suffix))
			content := readJailFile(ctx, path)
			Expect(content).NotTo(ContainSubstring("Traceback"))
		}
	})

	suite.And(ctx, "the passive cleanup removes the test /dev/shm file after the GPU job", func(ctx SpecContext) {
		Eventually(func() string {
			out, err := suite.ExecWorker(ctx, state.workerName, fmt.Sprintf("if [ -e /dev/shm/%s ]; then echo present; fi", state.shmMarker))
			Expect(err).NotTo(HaveOccurred())
			return strings.TrimSpace(out)
		}, 3*time.Minute, 10*time.Second).Should(BeEmpty())
	})
}
