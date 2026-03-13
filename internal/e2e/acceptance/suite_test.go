//go:build acceptance

package acceptance

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

var suite *framework.Suite

func TestAcceptance(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Soperator Acceptance Suite")
}

var _ = BeforeSuite(func(ctx SpecContext) {
	var err error
	suite, err = framework.LoadSuite(ctx)
	Expect(err).NotTo(HaveOccurred())
}, NodeTimeout(5*time.Minute))

var _ = ReportAfterSuite("write acceptance summary", func(report types.Report) {
	if suite == nil {
		return
	}

	Expect(suite.WriteSummary(report)).To(Succeed())
})

var _ = Describe("Acceptance", Ordered, Serial, func() {
	It("confirms that a provisioned cluster is ready for acceptance testing", clusterCreationTest)
	It("confirms that a regular user can SSH from the login node to a worker", internalSSHTest)
	It("confirms that installing jq does not break GPU tooling", packageInstallationTest)
	It("confirms that a selected worker is replaced after a maintenance event", nodeReplacementTest)
})
