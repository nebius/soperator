//go:build acceptance

package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

func submitBatchJob(ctx SpecContext, command string, args ...string) string {
	baseArgs := []string{"sbatch", "--parsable"}
	baseArgs = append(baseArgs, args...)
	baseArgs = append(baseArgs, "--wrap", command)

	out, err := suite.ExecJail(ctx, shellCommand(baseArgs...))
	Expect(err).NotTo(HaveOccurred())

	jobID := strings.TrimSpace(out)
	Expect(jobID).NotTo(BeEmpty())
	suite.Logf("submitted Slurm job %s with command %q", jobID, command)

	return jobID
}

func waitForJobState(ctx SpecContext, jobID, wantState string, timeout time.Duration) {
	Eventually(func(ctx context.Context) (string, error) {
		out, err := suite.ExecController(ctx, fmt.Sprintf("squeue -h -j %s -o '%%T'", framework.ShellQuote(jobID)))
		return strings.TrimSpace(out), err
	}, timeout, 10*time.Second).WithContext(ctx).Should(ContainSubstring(wantState))
}

func waitForJobCompletion(ctx SpecContext, jobID string, timeout time.Duration) {
	Eventually(func(ctx context.Context) (string, error) {
		out, err := suite.ExecController(ctx, fmt.Sprintf("sacct -X -n -j %s -o State", framework.ShellQuote(jobID)))
		if err != nil {
			return "", err
		}

		for _, line := range shellLines(out) {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			return fields[0], nil
		}

		return "", nil
	}, timeout, 15*time.Second).WithContext(ctx).Should(SatisfyAny(
		Equal("COMPLETED"),
		Equal("FAILED"),
		Equal("CANCELLED"),
		Equal("TIMEOUT"),
	))
}

func cancelJob(jobID string) {
	if jobID == "" {
		return
	}

	if _, err := suite.ExecController(context.Background(), fmt.Sprintf("scancel %s || true", framework.ShellQuote(jobID))); err != nil {
		suite.Logf("cleanup: cancel job %s: %v", jobID, err)
	}
}

func latestJailFile(ctx SpecContext, pattern string) string {
	out, err := suite.ExecJail(ctx, fmt.Sprintf("ls -1t %s 2>/dev/null | head -n1", pattern))
	Expect(err).NotTo(HaveOccurred())

	path := strings.TrimSpace(out)
	Expect(path).NotTo(BeEmpty(), "expected a file matching %s", pattern)

	return path
}

func readJailFile(ctx SpecContext, path string) string {
	out, err := suite.ExecJail(ctx, fmt.Sprintf("cat %s", framework.ShellQuote(path)))
	Expect(err).NotTo(HaveOccurred())
	return out
}

func shellLines(out string) []string {
	lines := strings.Split(out, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		result = append(result, line)
	}
	return result
}

func shellCommand(args ...string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, framework.ShellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func jsonLinesByFile(content string) []map[string]any {
	results := make([]map[string]any, 0)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !json.Valid([]byte(line)) {
			continue
		}

		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err == nil {
			results = append(results, payload)
		}
	}

	return results
}

func jsonStringField(payload map[string]any, keys ...string) string {
	current := any(payload)
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = asMap[key]
		if !ok {
			return ""
		}
	}

	value, ok := current.(string)
	if !ok {
		return ""
	}

	return value
}

func triggerCronJob(ctx SpecContext, cronJobName, prefix string) string {
	jobName := fmt.Sprintf("%s-%d", prefix, time.Now().Unix())
	_, err := suite.Run(
		ctx,
		"kubectl", "create", "job",
		"-n", "soperator",
		"--from=cronjob/"+cronJobName,
		jobName,
	)
	Expect(err).NotTo(HaveOccurred())
	suite.Logf("triggered cronjob %s as job %s", cronJobName, jobName)

	return jobName
}

func waitForK8sJobComplete(ctx SpecContext, jobName string, timeout time.Duration) {
	Eventually(func(ctx context.Context) (string, error) {
		out, err := suite.Run(
			ctx,
			"kubectl", "get", "job", "-n", "soperator", jobName,
			"-o", "jsonpath={.status.conditions[?(@.type=='Complete')].status}",
		)
		return strings.TrimSpace(out), err
	}, timeout, 10*time.Second).WithContext(ctx).Should(Equal("True"))
}

func latestJobPod(ctx SpecContext, jobName string) string {
	out, err := suite.Run(
		ctx,
		"kubectl", "get", "pods", "-n", "soperator",
		"-l", "job-name="+jobName,
		"-o", "jsonpath={.items[0].metadata.name}",
	)
	Expect(err).NotTo(HaveOccurred())

	podName := strings.TrimSpace(out)
	Expect(podName).NotTo(BeEmpty())

	return podName
}

func activeCheckK8sStatus(ctx context.Context, checkName string) (string, error) {
	out, err := suite.Run(
		ctx,
		"kubectl", "get", "activecheck", "-n", "soperator", checkName,
		"-o", "jsonpath={.status.k8sJobsStatus.lastJobStatus}",
	)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(out), nil
}

func activeCheckSlurmStatus(ctx context.Context, checkName string) (string, string, error) {
	out, err := suite.Run(
		ctx,
		"kubectl", "get", "activecheck", "-n", "soperator", checkName,
		"-o", "jsonpath={.status.slurmJobsStatus.lastRunName}|{.status.slurmJobsStatus.lastRunStatus}",
	)
	if err != nil {
		return "", "", err
	}

	parts := strings.SplitN(strings.TrimSpace(out), "|", 2)
	if len(parts) != 2 {
		return "", "", nil
	}

	return parts[0], parts[1], nil
}
