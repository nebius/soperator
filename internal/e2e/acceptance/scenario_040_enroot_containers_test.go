//go:build acceptance

package acceptance

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const (
	enrootScenarioImage = "docker://cr.eu-north1.nebius.cloud#soperator/active_checks:12.9.0-ubuntu24.04-nccl_tests2.16.4-3935b93"
	enrootMounts        = "/usr/lib/x86_64-linux-gnu:/usr/lib/x86_64-linux-gnu,/usr/lib64:/usr/lib64"
)

func enrootContainersTest(ctx SpecContext) {
	state := struct {
		workerName string
		jobID      string
	}{}

	suite.Given(ctx, "a worker is selected for the Enroot container check", func(_ SpecContext) {
		worker, err := suite.AnyWorker()
		Expect(err).NotTo(HaveOccurred())

		state.workerName = worker.Name
		suite.Logf("using worker %s for Enroot container validation", state.workerName)
	})

	suite.When(ctx, "a long-running Enroot-based Slurm job is submitted", func(ctx SpecContext) {
		command := fmt.Sprintf(
			"srun --container-image=%s --container-mounts=%s bash -lc 'sleep 600'",
			framework.ShellQuote(enrootScenarioImage),
			framework.ShellQuote(enrootMounts),
		)
		state.jobID = submitBatchJob(
			ctx,
			command,
			"-N", "1",
			"-w", state.workerName,
			"--job-name", "e2e-s040-enroot",
		)
	})

	DeferCleanup(func() {
		cancelJob(state.jobID)
	})

	suite.Then(ctx, "the Enroot-based job reaches the RUNNING state", func(ctx SpecContext) {
		waitForJobState(ctx, state.jobID, "RUNNING", 15*time.Minute)
	})

	suite.And(ctx, "Enroot runtime and cache artifacts appear", func(ctx SpecContext) {
		_, err := suite.ExecWorker(ctx, state.workerName, "find /mnt/image-storage/enroot/cache -mindepth 1 -maxdepth 2 | head -n1")
		Expect(err).NotTo(HaveOccurred())

		_, err = suite.ExecWorker(ctx, state.workerName, "find /mnt/image-storage/enroot/data -mindepth 1 -maxdepth 1 | head -n1")
		Expect(err).NotTo(HaveOccurred())

		_, err = suite.ExecJail(ctx, "find /var/cache/enroot-container-images -name '*.sqsh' -o -name '*.squashfs' | head -n1")
		Expect(err).NotTo(HaveOccurred())
	})

	suite.When(ctx, "the Enroot-based Slurm job is cancelled", func(_ SpecContext) {
		cancelJob(state.jobID)
		state.jobID = ""
	})

	suite.Then(ctx, "temporary Enroot runtime data is cleaned up after cancellation", func(ctx SpecContext) {
		Eventually(func() string {
			out, err := suite.ExecWorker(ctx, state.workerName, "find /mnt/image-storage/enroot/data -mindepth 1 -maxdepth 1 | head -n5 || true")
			Expect(err).NotTo(HaveOccurred())
			return strings.TrimSpace(out)
		}, 5*time.Minute, 10*time.Second).Should(BeEmpty())
	})
}
