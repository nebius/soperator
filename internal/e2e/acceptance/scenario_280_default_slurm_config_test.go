//go:build acceptance

package acceptance

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func defaultSlurmConfigTest(ctx SpecContext) {
	var config string

	suite.When(ctx, "the active Slurm config is queried", func(ctx SpecContext) {
		out, err := suite.ExecController(ctx, "scontrol show config")
		Expect(err).NotTo(HaveOccurred())

		config = out
		Expect(strings.TrimSpace(config)).NotTo(BeEmpty())
	})

	suite.Then(ctx, "the config contains plugin settings", func(_ SpecContext) {
		Expect(config).To(ContainSubstring("Plugin"))
	})

	suite.And(ctx, "the config exposes Slurm script settings", func(_ SpecContext) {
		Expect(config).To(ContainSubstring("Prolog"))
		Expect(config).To(ContainSubstring("Epilog"))
		Expect(config).To(ContainSubstring("HealthCheckProgram"))
	})

	suite.And(ctx, "the config exposes resource and timeout settings", func(_ SpecContext) {
		Expect(config).To(ContainSubstring("DefMem"))
		Expect(config).To(ContainSubstring("Gres"))
		Expect(config).To(ContainSubstring("Timeout"))
	})
}
