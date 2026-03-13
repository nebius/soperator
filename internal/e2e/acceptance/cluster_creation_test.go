//go:build acceptance

package acceptance

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

func clusterCreationTest(ctx SpecContext) {
	suite.Step(ctx, "verifying suite discovery found worker nodes", func(_ SpecContext, step *framework.StepRecorder) {
		step.Detail("worker_count", fmt.Sprintf("%d", suite.WorkerCount()))
		Expect(suite.WorkerCount()).To(BeNumerically(">", 0))
		suite.Logf("cluster has %d workers", suite.WorkerCount())
	})
}
