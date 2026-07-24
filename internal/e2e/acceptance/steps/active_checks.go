package steps

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const (
	activeLogsCleanerSuffix = "soperator-outputs-logs-cleaner"
	activeGPUChecksSuffix   = "gpu-checks"

	activeK8sJobTimeout        = 5 * time.Minute
	activeGPUCheckTimeout      = 40 * time.Minute
	activeSlurmAccountingWait  = 3 * time.Minute
	activeOutputCleanupTimeout = 3 * time.Minute
)

type ActiveChecks struct {
	state *framework.ClusterState
	exec  framework.Exec

	oldOutputFile string

	gpuCheckJobName string
	gpuCheckJobIDs  []string
}

func NewActiveChecks(state *framework.ClusterState, exec framework.Exec) *ActiveChecks {
	return &ActiveChecks{
		state: state,
		exec:  exec,
	}
}

func (s *ActiveChecks) Register(sc *godog.ScenarioContext) {
	sc.After(func(ctx context.Context, scenario *godog.Scenario, err error) (context.Context, error) {
		s.cleanup(context.Background())
		return ctx, nil
	})

	sc.Step(`^an old soperator output file is created for acceptance$`, s.anOldSoperatorOutputFileIsCreatedForAcceptance)
	sc.Step(`^the logs cleaner ActiveCheck is triggered$`, s.theLogsCleanerActiveCheckIsTriggered)
	sc.Step(`^the old soperator output file is removed$`, s.theOldSoperatorOutputFileIsRemoved)
	sc.Step(`^GPU workers are available for active checks$`, s.gpuWorkersAreAvailableForActiveChecks)
	sc.Step(`^the GPU ActiveCheck is triggered$`, s.theGPUActiveCheckIsTriggered)
	sc.Step(`^the GPU ActiveCheck finishes successfully on all GPU workers$`, s.theGPUActiveCheckFinishesSuccessfullyOnAllGPUWorkers)
}

func (s *ActiveChecks) anOldSoperatorOutputFileIsCreatedForAcceptance(ctx context.Context) error {
	s.oldOutputFile = fmt.Sprintf("/opt/soperator-outputs/acceptance-old-file-%d.txt", time.Now().UnixNano())
	cmd := fmt.Sprintf("touch %s && touch -m -d '2025-01-01 12:34:56' %s && ls -la %s",
		framework.ShellQuote(s.oldOutputFile),
		framework.ShellQuote(s.oldOutputFile),
		framework.ShellQuote(s.oldOutputFile),
	)
	if _, err := s.exec.Jail().RunWithDefaultRetry(ctx, cmd); err != nil {
		return fmt.Errorf("create old soperator output file %s: %w", s.oldOutputFile, err)
	}
	return nil
}

func (s *ActiveChecks) theLogsCleanerActiveCheckIsTriggered(ctx context.Context) error {
	jobName, err := s.createJobFromCronJob(ctx, s.activeCheckName(activeLogsCleanerSuffix))
	if err != nil {
		return err
	}
	if err := s.waitForK8sJobComplete(ctx, jobName, activeK8sJobTimeout); err != nil {
		return err
	}
	return nil
}

func (s *ActiveChecks) gpuWorkersAreAvailableForActiveChecks() error {
	if len(s.state.GPUWorkers) == 0 {
		return fmt.Errorf("no GPU workers discovered for GPU ActiveCheck validation")
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
	s.gpuCheckJobName = jobName
	s.gpuCheckJobIDs = jobIDs
	return nil
}

func (s *ActiveChecks) theGPUActiveCheckFinishesSuccessfullyOnAllGPUWorkers(ctx context.Context) error {
	if s.gpuCheckJobName == "" || len(s.gpuCheckJobIDs) == 0 {
		return fmt.Errorf("GPU ActiveCheck job is not captured")
	}
	if err := s.waitForK8sJobComplete(ctx, s.gpuCheckJobName, activeGPUCheckTimeout); err != nil {
		return err
	}
	if err := s.waitForActiveCheckSlurmRunComplete(ctx, s.activeCheckName(activeGPUChecksSuffix), s.gpuCheckJobIDs[0], activeGPUCheckTimeout); err != nil {
		return err
	}

	// Do not read /opt/soperator-outputs/slurm_jobs here: the jail log collector
	// may delete those files after read while long GPU checks are still running.
	if err := s.waitForGPUActiveCheckSlurmJobsCompletedOnAllGPUWorkers(ctx); err != nil {
		return err
	}

	s.gpuCheckJobName = ""
	s.gpuCheckJobIDs = nil
	return nil
}

func (s *ActiveChecks) theOldSoperatorOutputFileIsRemoved(ctx context.Context) error {
	if s.oldOutputFile == "" {
		return fmt.Errorf("old output file path is not captured")
	}
	if err := s.exec.WaitFor(ctx, "old soperator output file removed", activeOutputCleanupTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		out, err := s.exec.Jail().RunWithDefaultRetry(waitCtx,
			fmt.Sprintf("test ! -e %s && echo removed || true", framework.ShellQuote(s.oldOutputFile)))
		if err != nil {
			return false, err
		}
		return strings.TrimSpace(out) == "removed", nil
	}); err != nil {
		return err
	}
	s.oldOutputFile = ""
	return nil
}

func (s *ActiveChecks) cleanup(ctx context.Context) {
	if s.oldOutputFile != "" {
		if _, err := s.exec.Jail().Run(ctx, fmt.Sprintf("rm -f %s >/dev/null 2>&1 || true", framework.ShellQuote(s.oldOutputFile))); err != nil {
			s.exec.Logf("cleanup: remove old output file %s: %v", s.oldOutputFile, err)
		} else {
			s.oldOutputFile = ""
		}
	}
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

func (s *ActiveChecks) waitForK8sJobComplete(ctx context.Context, jobName string, timeout time.Duration) error {
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
		case consts.ActiveCheckSlurmRunStatusFailed, consts.ActiveCheckSlurmRunStatusCancelled, consts.ActiveCheckSlurmRunStatusError:
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
	err := s.exec.WaitFor(ctx, "GPU ActiveCheck Slurm jobs completed on all GPU workers", activeSlurmAccountingWait, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
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
		return true, nil
	})
	if err != nil {
		return err
	}
	return assertGPUActiveCheckSlurmRecords(records, s.gpuCheckJobIDs, s.state.GPUWorkers)
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
	expected := make(map[string]struct{}, len(expectedWorkers))
	for _, worker := range expectedWorkers {
		expected[worker.Name] = struct{}{}
	}
	completed := make(map[string]string, len(expectedWorkers))
	var problems []string

	for _, jobID := range jobIDs {
		record, ok := records[jobID]
		if !ok {
			problems = append(problems, fmt.Sprintf("job %s is missing from sacct", jobID))
			continue
		}
		if record.State != "COMPLETED" || record.ExitCode != "0:0" {
			problems = append(problems, fmt.Sprintf("job %s finished with state=%s exit_code=%s node=%s", jobID, record.State, record.ExitCode, record.NodeList))
			continue
		}
		if _, ok := expected[record.NodeList]; !ok {
			problems = append(problems, fmt.Sprintf("job %s completed on unexpected node %s", jobID, record.NodeList))
			continue
		}
		completed[record.NodeList] = jobID
	}
	for worker := range expected {
		if _, ok := completed[worker]; !ok {
			problems = append(problems, fmt.Sprintf("GPU worker %s has no completed ActiveCheck job", worker))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
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
