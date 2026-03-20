//go:build acceptance

package acceptance

import (
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func networkTopologyTest(ctx SpecContext) {
	var topologyOutput string
	var workers []string

	suite.Given(ctx, "workers are available for topology validation", func(_ SpecContext) {
		selected, err := suite.WorkersForJob(2)
		Expect(err).NotTo(HaveOccurred())
		Expect(selected).NotTo(BeEmpty())

		for _, worker := range selected {
			workers = append(workers, worker.Name)
		}
		suite.Logf("validating topology for workers: %s", strings.Join(workers, ", "))
	})

	suite.When(ctx, "the Slurm topology is queried", func(ctx SpecContext) {
		out, err := suite.ExecController(ctx, "scontrol show topology")
		Expect(err).NotTo(HaveOccurred())

		topologyOutput = strings.TrimSpace(out)
		Expect(topologyOutput).NotTo(BeEmpty())
	})

	suite.Then(ctx, "the discovered workers appear in the topology output", func(_ SpecContext) {
		for _, worker := range workers {
			Expect(topologyOutput).To(ContainSubstring(worker))
		}
	})

	suite.And(ctx, "the topology output does not assign workers to an unknown group", func(_ SpecContext) {
		lower := strings.ToLower(topologyOutput)
		Expect(lower).NotTo(ContainSubstring("nodes=unknown"))
		Expect(lower).NotTo(ContainSubstring("switchname=unknown"))
	})

	suite.Then(ctx, "topology information is propagated into a running job", func(ctx SpecContext) {
		jobID := submitBatchJob(
			ctx,
			"srun bash -lc 'echo ${SLURM_TOPOLOGY_ADDR}'",
			"-N", strconv.Itoa(len(workers)),
			"--job-name", "e2e-s100-topology",
		)
		DeferCleanup(func() { cancelJob(jobID) })

		waitForJobCompletion(ctx, jobID, 10*time.Minute)

		out, err := suite.ExecJail(ctx, "cat slurm-"+jobID+".out")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).NotTo(BeEmpty())
		Expect(out).To(ContainSubstring("root."))
	})
}
