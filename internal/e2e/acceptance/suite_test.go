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

var _ = SynchronizedBeforeSuite(func(ctx SpecContext) []byte {
	setupSuite := framework.NewSuite()
	setupSuite.BeginSpec()
	defer setupSuite.FlushSetupLogs()

	Expect(setupSuite.DiscoverCluster(ctx)).To(Succeed())

	data, err := setupSuite.EncodeWorkers()
	Expect(err).NotTo(HaveOccurred())

	return data
}, func(_ SpecContext, data []byte) {
	suite = framework.NewSuite()
	Expect(suite.LoadWorkers(data)).To(Succeed())
}, NodeTimeout(5*time.Minute))

var _ = ReportBeforeEach(func(report types.SpecReport) {
	if suite == nil || !report.LeafNodeType.Is(types.NodeTypeIt) {
		return
	}

	suite.BeginSpec()
})

var _ = ReportAfterEach(func(report types.SpecReport) {
	if suite == nil || !report.LeafNodeType.Is(types.NodeTypeIt) {
		return
	}

	suite.FlushSpecLogs()
})

var _ = ReportAfterSuite("write acceptance summary", func(report types.Report) {
	if suite == nil {
		return
	}

	Expect(suite.WriteSummary(report)).To(Succeed())
})

var _ = Describe("Simple acceptance", func() {
	It("confirms that a provisioned cluster is ready for acceptance testing", clusterCreationTest)
	It("confirms that a regular user can SSH from the login node to a worker", internalSSHTest)
	It("confirms that installing jq does not break GPU tooling", packageInstallationTest)
})

var _ = Describe("Node replacement acceptance", Serial, func() {
	It("confirms that a selected worker is replaced after a maintenance event", nodeReplacementTest)
})
