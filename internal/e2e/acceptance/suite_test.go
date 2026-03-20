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
	It("Scenario 050: confirms that installing jq does not break GPU tooling", packageInstallationTest)
	It("Scenario 100: confirms that Slurm network topology is exposed to jobs", networkTopologyTest)
	It("Scenario 210: confirms that the expected filesystem layout is present", filesystemLayoutTest)
	It("Scenario 220: confirms that a regular user can SSH from the login node to a worker", internalSSHTest)
	It("Scenario 280: confirms that the active Slurm config looks sane", defaultSlurmConfigTest)
})

var _ = Describe("Serial acceptance", Serial, func() {
	It("Scenario 030: confirms that Docker-based Slurm containers start and clean up correctly", dockerContainersTest)
	It("Scenario 040: confirms that Enroot-based Slurm containers start and clean up correctly", enrootContainersTest)
	It("Scenario 080: confirms that a selected worker is replaced after a maintenance event", nodeReplacementTest)
	It("Scenario 130: confirms that GPU health checks produce healthy outputs for the current cluster", gpuHealthChecksTest)
	It("Scenario 131: confirms that passive checks run for periodic, CPU, and GPU job paths", passiveChecksTest)
	It("Scenario 132: confirms that selected ActiveChecks can be triggered and report expected results", activeChecksTest)
})
