//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

func activeChecksTest(ctx SpecContext) {
	state := struct {
		workerName               string
		retriggerJobName         string
		createUserJobName        string
		ensureDirJobName         string
		ensureHealthyBaselineRun string
	}{}

	suite.Given(ctx, "ActiveCheck resources are present", func(ctx SpecContext) {
		out, err := suite.Run(ctx, "kubectl", "get", "activechecks", "-n", "soperator", "-o", "name")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).NotTo(BeEmpty())

		worker, err := suite.AnyWorker()
		Expect(err).NotTo(HaveOccurred())
		state.workerName = worker.Name
	})

	suite.When(ctx, "the retrigger-checks CronJob is executed", func(ctx SpecContext) {
		state.retriggerJobName = triggerCronJob(ctx, "retrigger-checks", "e2e-s132-retrigger")
	})

	suite.Then(ctx, "the retrigger job completes", func(ctx SpecContext) {
		waitForK8sJobComplete(ctx, state.retriggerJobName, 15*time.Minute)
	})

	suite.When(ctx, "the create-user-nebius and ensure-dir-snccld-logs CronJobs are executed directly", func(ctx SpecContext) {
		state.createUserJobName = triggerCronJob(ctx, "create-user-nebius", "e2e-s132-user")
		state.ensureDirJobName = triggerCronJob(ctx, "ensure-dir-snccld-logs", "e2e-s132-dir")
	})

	suite.Then(ctx, "the selected K8s ActiveChecks complete successfully", func(ctx SpecContext) {
		waitForK8sJobComplete(ctx, state.createUserJobName, 15*time.Minute)
		waitForK8sJobComplete(ctx, state.ensureDirJobName, 15*time.Minute)

		Eventually(func(ctx context.Context) (string, error) {
			return activeCheckK8sStatus(ctx, "create-user-nebius")
		}, 5*time.Minute, 10*time.Second).WithContext(ctx).Should(Equal("Complete"))

		Eventually(func(ctx context.Context) (string, error) {
			return activeCheckK8sStatus(ctx, "ensure-dir-snccld-logs")
		}, 5*time.Minute, 10*time.Second).WithContext(ctx).Should(Equal("Complete"))
	})

	suite.And(ctx, "the selected K8s ActiveChecks have the expected side effects", func(ctx SpecContext) {
		_, err := suite.ExecJail(ctx, "id nebius")
		Expect(err).NotTo(HaveOccurred())

		out, err := suite.ExecJail(ctx, "stat -c '%a %n' /opt/soperator-outputs/nccl_logs")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).To(HavePrefix("777 "))
	})

	suite.When(ctx, "a worker is manually drained before ensure-healthy-nodes runs", func(ctx SpecContext) {
		runName, _, err := activeCheckSlurmStatus(ctx, "ensure-healthy-nodes")
		Expect(err).NotTo(HaveOccurred())
		state.ensureHealthyBaselineRun = runName

		_, err = suite.ExecController(ctx, fmt.Sprintf(
			"scontrol update nodename=%s state=drain reason=%s",
			framework.ShellQuote(state.workerName),
			framework.ShellQuote("e2e-s132"),
		))
		Expect(err).NotTo(HaveOccurred())

		triggerCronJob(ctx, "ensure-healthy-nodes", "e2e-s132-healthy")
	})

	DeferCleanup(func() {
		if state.workerName == "" {
			return
		}

		if _, err := suite.ExecController(context.Background(), fmt.Sprintf(
			"scontrol update nodename=%s state=resume",
			framework.ShellQuote(state.workerName),
		)); err != nil {
			suite.Logf("cleanup: resume worker %s: %v", state.workerName, err)
		}
	})

	suite.Then(ctx, "the ensure-healthy-nodes ActiveCheck reports failure", func(ctx SpecContext) {
		Eventually(func(ctx context.Context) (string, error) {
			runName, status, err := activeCheckSlurmStatus(ctx, "ensure-healthy-nodes")
			if err != nil {
				return "", err
			}
			if runName == "" || runName == state.ensureHealthyBaselineRun {
				return "", nil
			}

			return status, nil
		}, 15*time.Minute, 15*time.Second).WithContext(ctx).Should(Equal("Failed"))
	})
}
