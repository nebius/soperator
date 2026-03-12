//go:build acceptance

package acceptance

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
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
