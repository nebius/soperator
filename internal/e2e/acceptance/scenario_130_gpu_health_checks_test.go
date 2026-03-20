//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func gpuHealthChecksTest(ctx SpecContext) {
	var baselineRunName string

	suite.Given(ctx, "periodic GPU health-check outputs exist", func(ctx SpecContext) {
		out, err := suite.ExecJail(ctx, "find /opt/soperator-outputs/slurm_scripts -name 'worker-*.gpu_health_check.hc_program.out' -mmin -180 | head -n1")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).NotTo(BeEmpty())
	})

	suite.Then(ctx, "the periodic GPU health-check outputs report PASS with metadata", func(ctx SpecContext) {
		content, err := suite.ExecJail(ctx, "cat /opt/soperator-outputs/slurm_scripts/worker-*.gpu_health_check.hc_program.out 2>/dev/null")
		Expect(err).NotTo(HaveOccurred())

		results := jsonLinesByFile(content)
		Expect(results).NotTo(BeEmpty())

		runIDs := make([]string, 0, len(results))
		for _, result := range results {
			Expect(jsonStringField(result, "status")).To(Equal("PASS"))
			Expect(jsonStringField(result, "meta", "run_id")).NotTo(BeEmpty())
			runIDs = append(runIDs, jsonStringField(result, "meta", "run_id"))
		}

		for _, runID := range runIDs {
			_, err := suite.ExecJail(ctx, fmt.Sprintf("find /opt/soperator-outputs/health_checker_cmd_stdout -name '*.%s.out' | head -n1", runID))
			Expect(err).NotTo(HaveOccurred())
		}
	})

	suite.When(ctx, "the gpu-fryer ActiveCheck is triggered", func(ctx SpecContext) {
		runName, _, err := activeCheckSlurmStatus(ctx, "gpu-fryer")
		Expect(err).NotTo(HaveOccurred())
		baselineRunName = runName

		triggerCronJob(ctx, "gpu-fryer", "e2e-s130-gpu-fryer")
	})

	suite.Then(ctx, "the gpu-fryer ActiveCheck completes successfully", func(ctx SpecContext) {
		Eventually(func(ctx context.Context) (string, error) {
			runName, status, err := activeCheckSlurmStatus(ctx, "gpu-fryer")
			if err != nil {
				return "", err
			}
			if runName == "" || runName == baselineRunName {
				return "", nil
			}

			return status, nil
		}, 30*time.Minute, 20*time.Second).WithContext(ctx).Should(Equal("Complete"))
	})

	suite.And(ctx, "the latest gpu-fryer output files report PASS", func(ctx SpecContext) {
		out, err := suite.ExecJail(ctx, "cat /opt/soperator-outputs/slurm_jobs/worker-*.gpu-fryer.*.out 2>/dev/null")
		Expect(err).NotTo(HaveOccurred())

		results := jsonLinesByFile(out)
		Expect(results).NotTo(BeEmpty())

		foundPass := false
		for _, result := range results {
			if jsonStringField(result, "status") == "PASS" {
				foundPass = true
				break
			}
		}
		Expect(foundPass).To(BeTrue())
	})
}
