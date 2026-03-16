//go:build acceptance

package acceptance

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func clusterCreationTest(ctx SpecContext) {
	suite.Detail("worker_count", fmt.Sprintf("%d", suite.WorkerCount()))

	suite.Step(ctx, "verifying suite discovery found worker nodes", func(_ SpecContext) {
		Expect(suite.WorkerCount()).To(BeNumerically(">", 0))
		suite.Logf("cluster has %d workers", suite.WorkerCount())
	})
}
