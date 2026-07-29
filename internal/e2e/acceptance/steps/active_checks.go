package steps

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const (
	activeGPUChecksSuffix = "gpu-checks"

	activeK8sJobTimeout   = 5 * time.Minute
	activeGPUCheckTimeout = 40 * time.Minute
	activeSlurmOutputWait = 15 * time.Minute

	activeSlurmJobOutputDir = "/opt/soperator-outputs/slurm_jobs"
)

var activeGPUHealthStatusPattern = regexp.MustCompile(`(?m)^Health checker status:\s*(\S+)\s*$`)

type ActiveChecks struct {
	state *framework.ClusterState
	exec  framework.Exec
	slurm *framework.SlurmClient

	gpuCheckK8sJobName string
	gpuCheckJobIDs     []string
}

func NewActiveChecks(state *framework.ClusterState, exec framework.Exec, slurm *framework.SlurmClient) *ActiveChecks {
	return &ActiveChecks{
		state: state,
		exec:  exec,
		slurm: slurm,
	}
}

func (s *ActiveChecks) Register(sc *godog.ScenarioContext) {
	sc.Step(`^healthy GPU workers are available for active checks$`, s.healthyGPUWorkersAreAvailableForActiveChecks)
	sc.Step(`^the GPU ActiveCheck is triggered$`, s.theGPUActiveCheckIsTriggered)
	sc.Step(`^the GPU ActiveCheck Kubernetes Job succeeds$`, s.theGPUActiveCheckKubernetesJobSucceeds)
	sc.Step(`^GPU ActiveCheck outputs report PASS on all GPU workers$`, s.gpuActiveCheckOutputsReportPASSOnAllGPUWorkers)
	sc.Step(`^GPU ActiveCheck Slurm jobs complete on all GPU workers$`, s.gpuActiveCheckSlurmJobsCompleteOnAllGPUWorkers)
	sc.Step(`^the GPU ActiveCheck status is Complete$`, s.theGPUActiveCheckStatusIsComplete)
}

func (s *ActiveChecks) healthyGPUWorkersAreAvailableForActiveChecks(ctx context.Context) error {
	if len(s.state.GPUWorkers) == 0 {
		return fmt.Errorf("no GPU workers discovered for GPU ActiveCheck validation")
	}
	var problems []string
	for _, worker := range s.state.GPUWorkers {
		node, err := s.slurm.NodeInfo(ctx, worker.Name)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", worker.Name, err))
			continue
		}
		if !node.IsUsable() {
			problems = append(problems, fmt.Sprintf("%s: state=%s reason=%s", worker.Name, node.State, node.Reason))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func (s *ActiveChecks) theGPUActiveCheckIsTriggered(ctx context.Context) error {
	jobName, err := s.createJobFromCronJob(ctx, s.activeCheckName(activeGPUChecksSuffix))
	if err != nil {
		return err
	}
	jobIDs, err := s.waitForK8sJobSlurmJobIDs(ctx, jobName, activeK8sJobTimeout)
	if err != nil {
		return err
	}
	s.gpuCheckK8sJobName = jobName
	s.gpuCheckJobIDs = jobIDs
	return nil
}

func (s *ActiveChecks) theGPUActiveCheckKubernetesJobSucceeds(ctx context.Context) error {
	if s.gpuCheckK8sJobName == "" {
		return fmt.Errorf("GPU ActiveCheck job is not captured")
	}
	return s.waitForK8sJobCompleteSuccessfully(ctx, s.gpuCheckK8sJobName, activeGPUCheckTimeout)
}

func (s *ActiveChecks) theGPUActiveCheckStatusIsComplete(ctx context.Context) error {
	if len(s.gpuCheckJobIDs) == 0 {
		return fmt.Errorf("GPU ActiveCheck Slurm job IDs are not captured")
	}
	if err := s.waitForActiveCheckSlurmRunComplete(ctx, s.activeCheckName(activeGPUChecksSuffix), s.gpuCheckJobIDs[0], activeGPUCheckTimeout); err != nil {
		return err
	}
	s.gpuCheckK8sJobName = ""
	s.gpuCheckJobIDs = nil
	return nil
}

func (s *ActiveChecks) gpuActiveCheckSlurmJobsCompleteOnAllGPUWorkers(ctx context.Context) error {
	return s.waitForGPUActiveCheckSlurmJobsCompletedOnAllGPUWorkers(ctx)
}

func (s *ActiveChecks) gpuActiveCheckOutputsReportPASSOnAllGPUWorkers(ctx context.Context) error {
	return s.waitForGPUActiveCheckOutputsPassingOnAllGPUWorkers(ctx)
}

func (s *ActiveChecks) activeCheckName(suffix string) string {
	return fmt.Sprintf("%s-%s", s.state.SlurmClusterName, suffix)
}

func (s *ActiveChecks) createJobFromCronJob(ctx context.Context, cronJobName string) (string, error) {
	jobName := uniqueK8sJobName(strings.TrimPrefix(cronJobName, s.state.SlurmClusterName+"-"))
	if _, err := s.exec.Kubectl().RunWithDefaultRetry(ctx,
		"create", "job", "-n", clusterCreationNamespace,
		"--from=cronjob/"+cronJobName,
		jobName,
	); err != nil {
		return "", fmt.Errorf("trigger CronJob %s as Job %s: %w", cronJobName, jobName, err)
	}
	s.exec.Logf("active checks: triggered CronJob %s as Job %s", cronJobName, jobName)
	return jobName, nil
}

func (s *ActiveChecks) waitForK8sJobSlurmJobIDs(ctx context.Context, jobName string, timeout time.Duration) ([]string, error) {
	var latest k8sJobStatus
	err := s.exec.WaitFor(ctx, fmt.Sprintf("Kubernetes Job %s Slurm job IDs", jobName), timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		var job k8sJobStatus
		if err := kubectlJSON(waitCtx, s.exec, &job, "get", "job", "-n", clusterCreationNamespace, jobName, "-o", "json"); err != nil {
			return false, err
		}
		latest = job
		if job.Status.Failed > 0 {
			return false, fmt.Errorf("Kubernetes Job %s failed before Slurm job IDs were recorded: %+v", jobName, job.Status)
		}
		return len(slurmJobIDsFromAnnotation(job.Metadata.Annotations)) > 0, nil
	})
	if err != nil {
		return nil, err
	}
	return slurmJobIDsFromAnnotation(latest.Metadata.Annotations), nil
}

func (s *ActiveChecks) waitForK8sJobCompleteSuccessfully(ctx context.Context, jobName string, timeout time.Duration) error {
	return s.exec.WaitFor(ctx, fmt.Sprintf("Kubernetes Job %s complete", jobName), timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		var job k8sJobStatus
		if err := kubectlJSON(waitCtx, s.exec, &job, "get", "job", "-n", clusterCreationNamespace, jobName, "-o", "json"); err != nil {
			return false, err
		}
		if job.Status.Succeeded > 0 {
			return true, nil
		}
		if job.Status.Failed > 0 {
			return false, fmt.Errorf("Kubernetes Job %s failed: %+v", jobName, job.Status)
		}
		return false, nil
	})
}

func (s *ActiveChecks) waitForActiveCheckSlurmRunComplete(ctx context.Context, checkName, firstJobID string, timeout time.Duration) error {
	return s.exec.WaitFor(ctx, fmt.Sprintf("ActiveCheck %s Slurm run %s complete", checkName, firstJobID), timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		var check activeCheckStatus
		if err := kubectlJSON(waitCtx, s.exec, &check, "get", "activecheck", "-n", clusterCreationNamespace, checkName, "-o", "json"); err != nil {
			return false, err
		}
		status := check.Status.SlurmJobsStatus
		if status.LastRunID != firstJobID {
			return false, nil
		}
		switch status.LastRunStatus {
		case consts.ActiveCheckSlurmRunStatusComplete:
			return true, nil
		case consts.ActiveCheckSlurmRunStatusFailed,
			consts.ActiveCheckSlurmRunStatusCancelled,
			consts.ActiveCheckSlurmRunStatusError,
			consts.ActiveCheckSlurmRunStatusSkipped:
			return false, fmt.Errorf("ActiveCheck %s finished with status=%s fail_jobs=%+v error_jobs=%+v cancelled_jobs=%v",
				checkName, status.LastRunStatus, status.LastRunFailJobsAndReasons, status.LastRunErrorJobsAndReasons, status.LastRunCancelledJobs)
		default:
			return false, nil
		}
	})
}

func uniqueK8sJobName(prefix string) string {
	prefix = strings.Trim(strings.ToLower(prefix), "-")
	if prefix == "" {
		prefix = "job"
	}
	name := fmt.Sprintf("acceptance-%s-%d", prefix, time.Now().Unix())
	if len(name) <= 63 {
		return name
	}
	return name[:63]
}

func slurmJobIDsFromAnnotation(annotations map[string]string) []string {
	raw := strings.TrimSpace(annotations["slurm-job-id"])
	if raw == "" {
		return nil
	}
	var ids []string
	for _, part := range strings.Split(raw, ",") {
		id := strings.TrimSpace(part)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *ActiveChecks) waitForGPUActiveCheckSlurmJobsCompletedOnAllGPUWorkers(ctx context.Context) error {
	var records map[string]activeCheckSlurmJobRecord
	err := s.exec.WaitFor(ctx, "GPU ActiveCheck Slurm jobs completed on all GPU workers", activeGPUCheckTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		next, err := s.gpuActiveCheckSlurmJobRecords(waitCtx)
		if err != nil {
			return false, err
		}
		records = next
		for _, jobID := range s.gpuCheckJobIDs {
			if _, ok := records[jobID]; !ok {
				return false, nil
			}
		}
		return activeCheckSlurmJobRecordsTerminal(records, s.gpuCheckJobIDs), nil
	})
	if err != nil {
		return err
	}
	return assertGPUActiveCheckSlurmRecords(records, s.gpuCheckJobIDs, s.state.GPUWorkers)
}

func (s *ActiveChecks) waitForGPUActiveCheckOutputsPassingOnAllGPUWorkers(ctx context.Context) error {
	if len(s.gpuCheckJobIDs) == 0 {
		return fmt.Errorf("GPU ActiveCheck Slurm job IDs are not captured")
	}
	var records map[string]activeCheckSlurmJobRecord
	if err := s.exec.WaitFor(ctx, "GPU ActiveCheck output files report PASS", activeSlurmOutputWait, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		next, err := s.gpuActiveCheckSlurmJobRecords(waitCtx)
		if err != nil {
			return false, err
		}
		if err := assertGPUActiveCheckSlurmRecordsTargetExpectedWorkers(next, s.gpuCheckJobIDs, s.state.GPUWorkers); err != nil {
			return false, err
		}
		for _, jobID := range s.gpuCheckJobIDs {
			record := next[jobID]
			out, exists, err := readJailFileIfExists(waitCtx, s.exec, activeCheckSlurmJobOutputPath(record))
			if err != nil {
				return false, err
			}
			if !exists {
				return false, nil
			}
			status, found := activeGPUCheckOutputStatus(out)
			if !found {
				return false, nil
			}
			if status != "PASS" {
				return false, fmt.Errorf("expected GPU ActiveCheck health-check status PASS, got %s:\n%s", status, strings.TrimSpace(out))
			}
		}
		records = next
		return true, nil
	}); err != nil {
		return err
	}
	return assertGPUActiveCheckSlurmRecordsTargetExpectedWorkers(records, s.gpuCheckJobIDs, s.state.GPUWorkers)
}

func (s *ActiveChecks) gpuActiveCheckSlurmJobRecords(ctx context.Context) (map[string]activeCheckSlurmJobRecord, error) {
	out, err := s.exec.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf(
		"sacct -j %s --noheader --parsable2 --format=JobID,JobName,State,ExitCode,NodeList 2>/dev/null || true",
		framework.ShellQuote(strings.Join(s.gpuCheckJobIDs, ",")),
	))
	if err != nil {
		return nil, fmt.Errorf("query GPU ActiveCheck Slurm jobs: %w", err)
	}
	return parseActiveCheckSlurmJobRecords(out), nil
}

func assertGPUActiveCheckSlurmRecords(records map[string]activeCheckSlurmJobRecord, jobIDs []string, expectedWorkers []framework.WorkerRef) error {
	if err := assertGPUActiveCheckSlurmRecordsTargetExpectedWorkers(records, jobIDs, expectedWorkers); err != nil {
		return err
	}

	var problems []string
	for _, jobID := range jobIDs {
		record := records[jobID]
		if record.State != "COMPLETED" || record.ExitCode != "0:0" {
			problems = append(problems, fmt.Sprintf("job %s finished with state=%s exit_code=%s node=%s", jobID, record.State, record.ExitCode, record.NodeList))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func assertGPUActiveCheckSlurmRecordsTargetExpectedWorkers(records map[string]activeCheckSlurmJobRecord, jobIDs []string, expectedWorkers []framework.WorkerRef) error {
	expected := make(map[string]struct{}, len(expectedWorkers))
	for _, worker := range expectedWorkers {
		expected[worker.Name] = struct{}{}
	}
	targeted := make(map[string]string, len(expectedWorkers))
	var problems []string

	for _, jobID := range jobIDs {
		record, ok := records[jobID]
		if !ok {
			problems = append(problems, fmt.Sprintf("job %s is missing from sacct", jobID))
			continue
		}
		if _, ok := expected[record.NodeList]; !ok {
			problems = append(problems, fmt.Sprintf("job %s targeted unexpected node %s", jobID, record.NodeList))
			continue
		}
		targeted[record.NodeList] = jobID
	}
	for worker := range expected {
		if _, ok := targeted[worker]; !ok {
			problems = append(problems, fmt.Sprintf("GPU worker %s has no ActiveCheck job", worker))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func activeCheckSlurmJobRecordsTerminal(records map[string]activeCheckSlurmJobRecord, jobIDs []string) bool {
	for _, jobID := range jobIDs {
		record, ok := records[jobID]
		if !ok || framework.IsJobAliveState(record.State) {
			return false
		}
	}
	return len(jobIDs) > 0
}

func parseActiveCheckSlurmJobRecords(output string) map[string]activeCheckSlurmJobRecord {
	records := map[string]activeCheckSlurmJobRecord{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, "|")
		if len(fields) < 5 {
			continue
		}
		id := strings.TrimSpace(fields[0])
		if id == "" || strings.Contains(id, ".") {
			continue
		}
		records[id] = activeCheckSlurmJobRecord{
			ID:       id,
			JobName:  strings.TrimSpace(fields[1]),
			State:    strings.TrimSpace(fields[2]),
			ExitCode: strings.TrimSpace(fields[3]),
			NodeList: strings.TrimSpace(fields[4]),
		}
	}
	return records
}

type k8sJobStatus struct {
	Metadata struct {
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Status struct {
		Succeeded int32 `json:"succeeded"`
		Failed    int32 `json:"failed"`
	} `json:"status"`
}

type activeCheckSlurmJobRecord struct {
	ID       string
	JobName  string
	State    string
	ExitCode string
	NodeList string
}

type activeCheckStatus struct {
	Status struct {
		SlurmJobsStatus struct {
			LastRunID                  string                           `json:"lastRunId"`
			LastRunStatus              consts.ActiveCheckSlurmRunStatus `json:"lastRunStatus"`
			LastRunFailJobsAndReasons  []any                            `json:"lastRunFailJobsAndReasons"`
			LastRunErrorJobsAndReasons []any                            `json:"lastRunErrorJobsAndReasons"`
			LastRunCancelledJobs       []string                         `json:"lastRunCancelledJobs"`
		} `json:"slurmJobsStatus"`
	} `json:"status"`
}

func activeCheckSlurmJobOutputPath(record activeCheckSlurmJobRecord) string {
	return fmt.Sprintf("%s/%s.%s.%s.out", activeSlurmJobOutputDir, record.NodeList, record.JobName, record.ID)
}

func assertActiveGPUCheckOutputPassing(output string) error {
	status, found := activeGPUCheckOutputStatus(output)
	if !found {
		return fmt.Errorf("GPU ActiveCheck output does not contain health-check status:\n%s", strings.TrimSpace(output))
	}
	if status != "PASS" {
		return fmt.Errorf("expected GPU ActiveCheck health-check status PASS, got %s:\n%s", status, strings.TrimSpace(output))
	}
	return nil
}

func activeGPUCheckOutputStatus(output string) (string, bool) {
	match := activeGPUHealthStatusPattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}
