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
	It("finds a provisioned cluster ready for acceptance tests", clusterCreationTest)
	It("allows a regular user to SSH to a worker without extra options", internalSSHTest)
	It("installs jq without breaking the NVIDIA driver", packageInstallationTest)
	It("replaces the selected worker after a maintenance event", nodeReplacementTest)
})
