//go:build acceptance

package acceptance

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func clusterCreationTest(ctx SpecContext) {
	suite.Step(ctx, "verifying suite discovery found worker nodes", "Confirmed that the provisioned cluster exposed at least one worker node for testing.", func() string {
		return "Worker count discovered during setup: " + fmt.Sprintf("%d", suite.WorkerCount())
	}, func(_ SpecContext) {
		Expect(suite.WorkerCount()).To(BeNumerically(">", 0))
		suite.Logf("cluster has %d workers", suite.WorkerCount())
	})
}
