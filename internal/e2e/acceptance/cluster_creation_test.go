//go:build acceptance

package acceptance

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster creation", func() {
	It("finds a provisioned cluster ready for acceptance tests", func() {
		By("verifying suite discovery found worker nodes")
		Expect(suite.WorkerCount()).To(BeNumerically(">", 0))
		suite.Logf("cluster has %d workers", suite.WorkerCount())
	})
})
