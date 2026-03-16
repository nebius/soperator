//go:build acceptance

package acceptance

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func clusterCreationTest(ctx SpecContext) {
	suite.Step(ctx, "verifying suite discovery found worker nodes", func(_ SpecContext) {
		Expect(suite.WorkerCount()).To(BeNumerically(">", 0))
		suite.Logf("cluster has %d workers", suite.WorkerCount())
	})
}
