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

const dockerScenarioImage = "cr.eu-north1.nebius.cloud/soperator/active_checks:12.9.0-ubuntu24.04-nccl_tests2.16.4-3935b93"

func dockerContainersTest(ctx SpecContext) {
	state := struct {
		workerName    string
		jobID         string
		containerName string
	}{
		containerName: fmt.Sprintf("e2e-s030-%d", time.Now().Unix()),
	}

	suite.Given(ctx, "a worker is selected for the Docker container check", func(_ SpecContext) {
		worker, err := suite.AnyWorker()
		Expect(err).NotTo(HaveOccurred())

		state.workerName = worker.Name
		suite.Logf("using worker %s for Docker container validation", state.workerName)
	})

	suite.When(ctx, "a long-running Docker-based Slurm job is submitted", func(ctx SpecContext) {
		command := fmt.Sprintf(
			"srun docker run --rm --name %s --gpus=all --device=/dev/infiniband %s bash -lc 'sleep 600'",
			state.containerName,
			dockerScenarioImage,
		)
		state.jobID = submitBatchJob(
			ctx,
			command,
			"-N", "1",
			"-w", state.workerName,
			"--job-name", "e2e-s030-docker",
		)
	})

	DeferCleanup(func() {
		cancelJob(state.jobID)
	})

	suite.Then(ctx, "the Docker-based job reaches the RUNNING state", func(ctx SpecContext) {
		waitForJobState(ctx, state.jobID, "RUNNING", 15*time.Minute)
	})

	suite.And(ctx, "Docker artifacts appear on the selected worker", func(ctx SpecContext) {
		Eventually(func() string {
			out, err := suite.ExecWorker(ctx, state.workerName, "docker ps --format '{{.Names}}'")
			Expect(err).NotTo(HaveOccurred())
			return strings.TrimSpace(out)
		}, 5*time.Minute, 10*time.Second).Should(ContainSubstring(state.containerName))

		_, err := suite.ExecWorker(ctx, state.workerName, fmt.Sprintf(
			"docker image ls --format '{{.Repository}}:{{.Tag}}' | grep -F %s",
			framework.ShellQuote(dockerScenarioImage),
		))
		Expect(err).NotTo(HaveOccurred())

		_, err = suite.ExecWorker(ctx, state.workerName, "find /mnt/image-storage/docker/rootfs/overlayfs -mindepth 1 -maxdepth 1 | head -n1")
		Expect(err).NotTo(HaveOccurred())

		_, err = suite.ExecWorker(ctx, state.workerName, "find /mnt/image-storage/docker/containerd/daemon/io.containerd.content.v1.content/blobs/sha256 -mindepth 1 -maxdepth 1 | head -n1")
		Expect(err).NotTo(HaveOccurred())
	})

	suite.When(ctx, "the Docker-based Slurm job is cancelled", func(_ SpecContext) {
		cancelJob(state.jobID)
		state.jobID = ""
	})

	suite.Then(ctx, "the worker no longer has the test container running", func(ctx SpecContext) {
		Eventually(func() string {
			out, err := suite.ExecWorker(ctx, state.workerName, "docker ps --format '{{.Names}}'")
			Expect(err).NotTo(HaveOccurred())
			return strings.TrimSpace(out)
		}, 5*time.Minute, 10*time.Second).ShouldNot(ContainSubstring(state.containerName))
	})
}
