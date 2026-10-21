package sharedsteps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	activeGPUChecksSuffix             = "gpu-checks"
	manageJailStateDryRunSuffix       = "manage-jail-state-dry-run"
	manageJailStateDryRunFile         = "/etc/profile.d/path_soperator_utils.sh"
	manageJailStateDryRunTask         = "Add soperator_utils to PATH for all users"
	manageJailStateDryRunLogsFilename = "manage-jail-state-dry-run.log"

	activeK8sJobTimeout     = 5 * time.Minute
	activeGPUCheckTimeout   = 25 * time.Minute
	manageJailDryRunTimeout = 65 * time.Minute
	activeSlurmOutputWait   = 20 * time.Minute
	activeSlurmOutputPoll   = 30 * time.Second

	activeSlurmJobOutputDir = "/opt/soperator-outputs/local/slurm_jobs"
)

var activeGPUHealthStatusPattern = regexp.MustCompile(`(?m)^Health checker status:\s*(\S+)\s*$`)

type ActiveChecks struct {
	info     *framework.ClusterInfo
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector

	gpuCheckK8sJobName string
	gpuCheckJobIDs     []string
	gpuWorkers         []framework.WorkerInfo

	dryRunJobName       string
	dryRunMarker        string
	dryRunChecksum      string
	dryRunLogs          string
	dryRunAnsibleResult ansibleDryRunResult
}

func NewActiveChecks(info *framework.ClusterInfo, runtime framework.Runtime, slurm *framework.SlurmClient, kubectl *framework.KubectlClient, selector *framework.WorkerSelector) *ActiveChecks {
	return &ActiveChecks{
		info:     info,
		runtime:  runtime,
		slurm:    slurm,
		kubectl:  kubectl,
		selector: selector,
	}
}

func (s *ActiveChecks) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^healthy GPU workers are available for active checks$`, s.healthyGPUWorkersAreAvailableForActiveChecks)
	sc.Step(`^the GPU ActiveCheck is triggered$`, s.theGPUActiveCheckIsTriggered)
	sc.Step(`^the GPU ActiveCheck Kubernetes Job succeeds$`, s.theGPUActiveCheckKubernetesJobSucceeds)
	sc.Step(`^GPU ActiveCheck outputs report PASS on all GPU workers$`, s.gpuActiveCheckOutputsReportPASSOnAllGPUWorkers)
	sc.Step(`^GPU ActiveCheck Slurm jobs complete on all GPU workers$`, s.gpuActiveCheckSlurmJobsCompleteOnAllGPUWorkers)
	sc.Step(`^the GPU ActiveCheck status is Complete$`, s.theGPUActiveCheckStatusIsComplete)
	sc.Step(`^the manage-jail-state dry-run ActiveCheck is configured$`, s.theManageJailStateDryRunActiveCheckIsConfigured)
	sc.Step(`^harmless drift is introduced into a managed jail file$`, s.harmlessDriftIsIntroducedIntoAManagedJailFile)
	sc.Step(`^the manage-jail-state dry-run ActiveCheck is triggered$`, s.theManageJailStateDryRunActiveCheckIsTriggered)
	sc.Step(`^the manage-jail-state dry-run Kubernetes Job succeeds$`, s.theManageJailStateDryRunKubernetesJobSucceeds)
	sc.Step(`^Ansible reports the managed jail file as changed$`, s.ansibleReportsTheManagedJailFileAsChanged)
	sc.Step(`^Ansible reports no failed or unreachable hosts$`, s.ansibleReportsNoFailedOrUnreachableHosts)
	sc.Step(`^the managed jail file remains unchanged by the dry-run$`, s.theManagedJailFileRemainsUnchangedByTheDryRun)
	sc.Step(`^the manage-jail-state dry-run ActiveCheck status is Complete$`, s.theManageJailStateDryRunActiveCheckStatusIsComplete)
}

func (s *ActiveChecks) CleanupAndReset(ctx context.Context) {
	if s.dryRunMarker != "" {
		expression := fmt.Sprintf(`\|^%s$|d`, s.dryRunMarker)
		command := fmt.Sprintf("sed -i -- %s %s",
			framework.ShellQuote(expression),
			framework.ShellQuote(manageJailStateDryRunFile),
		)
		if _, err := s.runtime.Jail().Run(ctx, command); err != nil {
			s.runtime.Logf("cleanup: remove marker from managed jail file %s: %v", manageJailStateDryRunFile, err)
		}
	}
	if s.dryRunJobName != "" {
		var job k8sJobStatus
		if err := s.kubectl.GetJSON(ctx, &job, "get", "job", "-n", framework.SoperatorNamespace, s.dryRunJobName, "-o", "json"); err != nil {
			s.runtime.Logf("cleanup: get manage-jail-state dry-run Job %s: %v", s.dryRunJobName, err)
		} else if job.Status.Succeeded == 0 && job.Status.Failed == 0 {
			if _, err := s.runtime.Kubectl().Run(ctx,
				"delete", "job", "-n", framework.SoperatorNamespace, s.dryRunJobName,
				"--ignore-not-found=true", "--wait=false",
			); err != nil {
				s.runtime.Logf("cleanup: stop manage-jail-state dry-run Job %s: %v", s.dryRunJobName, err)
			}
		}
	}

	s.gpuCheckK8sJobName = ""
	s.gpuCheckJobIDs = nil
	s.gpuWorkers = nil
	s.dryRunJobName = ""
	s.dryRunMarker = ""
	s.dryRunChecksum = ""
	s.dryRunLogs = ""
	s.dryRunAnsibleResult = ansibleDryRunResult{}
}

func (s *ActiveChecks) theManageJailStateDryRunActiveCheckIsConfigured(ctx context.Context) error {
	checkName := s.activeCheckName(manageJailStateDryRunSuffix)
	var check kubeobjects.ActiveCheck
	if err := s.kubectl.GetJSON(ctx, &check, "get", "activecheck", "-n", framework.SoperatorNamespace, checkName, "-o", "json"); err != nil {
		return fmt.Errorf("get manage-jail-state dry-run ActiveCheck %s: %w", checkName, err)
	}
	if check.Spec.CheckType != kubeobjects.ActiveCheckTypeK8sJob {
		return fmt.Errorf("expected ActiveCheck %s checkType=%s, got %s", checkName, kubeobjects.ActiveCheckTypeK8sJob, check.Spec.CheckType)
	}

	var cronJob activeCheckCronJob
	if err := s.kubectl.GetJSON(ctx, &cronJob, "get", "cronjob", "-n", framework.SoperatorNamespace, checkName, "-o", "json"); err != nil {
		return fmt.Errorf("get manage-jail-state dry-run CronJob %s: %w", checkName, err)
	}
	for _, container := range cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers {
		if slices.Contains(container.Args, "--check") {
			return nil
		}
	}
	return fmt.Errorf("expected CronJob %s to run ansible-playbook with --check", checkName)
}

func (s *ActiveChecks) harmlessDriftIsIntroducedIntoAManagedJailFile(ctx context.Context) error {
	nonce := time.Now().UnixNano()
	s.dryRunMarker = fmt.Sprintf("# acceptance-manage-jail-state-dry-run-%d", nonce)
	command := fmt.Sprintf(
		"printf '%%s\\n' %s >> %s && sha256sum %s | awk '{print $1}'",
		framework.ShellQuote(s.dryRunMarker),
		framework.ShellQuote(manageJailStateDryRunFile),
		framework.ShellQuote(manageJailStateDryRunFile),
	)
	checksum, err := s.runtime.Jail().Run(ctx, command)
	if err != nil {
		return fmt.Errorf("introduce harmless drift into managed jail file %s: %w", manageJailStateDryRunFile, err)
	}
	s.dryRunChecksum = strings.TrimSpace(checksum)
	if s.dryRunChecksum == "" {
		return fmt.Errorf("calculate checksum for drifted managed jail file %s", manageJailStateDryRunFile)
	}
	return nil
}

func (s *ActiveChecks) theManageJailStateDryRunActiveCheckIsTriggered(ctx context.Context) error {
	jobName, err := s.createJobFromCronJob(ctx, s.activeCheckName(manageJailStateDryRunSuffix))
	if err != nil {
		return err
	}
	s.dryRunJobName = jobName
	return nil
}

func (s *ActiveChecks) theManageJailStateDryRunKubernetesJobSucceeds(ctx context.Context) error {
	if s.dryRunJobName == "" {
		return fmt.Errorf("manage-jail-state dry-run Job is not captured")
	}
	waitErr := s.waitForK8sJobCompleteSuccessfully(ctx, s.dryRunJobName, manageJailDryRunTimeout)
	logsErr := s.captureDryRunJobLogs(ctx)
	if waitErr != nil {
		if logsErr != nil {
			return fmt.Errorf("%w; additionally, capture Job logs: %v", waitErr, logsErr)
		}
		return fmt.Errorf("%w\nJob logs:\n%s", waitErr, strings.TrimSpace(s.dryRunLogs))
	}
	return logsErr
}

func (s *ActiveChecks) ansibleReportsTheManagedJailFileAsChanged(ctx context.Context) error {
	if err := s.ensureDryRunAnsibleResult(ctx); err != nil {
		return err
	}
	if !s.dryRunAnsibleResult.ManagedFileChanged {
		return fmt.Errorf("Ansible did not report task %q as changed", manageJailStateDryRunTask)
	}
	return nil
}

func (s *ActiveChecks) ansibleReportsNoFailedOrUnreachableHosts(ctx context.Context) error {
	if err := s.ensureDryRunAnsibleResult(ctx); err != nil {
		return err
	}
	if !s.dryRunAnsibleResult.StatsFound {
		return fmt.Errorf("Ansible JSONL output does not contain v2_playbook_on_stats")
	}
	if s.dryRunAnsibleResult.Failures != 0 || s.dryRunAnsibleResult.Unreachable != 0 {
		return fmt.Errorf("expected no failed or unreachable Ansible hosts, got failures=%d unreachable=%d",
			s.dryRunAnsibleResult.Failures, s.dryRunAnsibleResult.Unreachable)
	}
	return nil
}

func (s *ActiveChecks) theManagedJailFileRemainsUnchangedByTheDryRun(ctx context.Context) error {
	if s.dryRunChecksum == "" || s.dryRunMarker == "" {
		return fmt.Errorf("managed jail file drift state is not captured")
	}
	command := fmt.Sprintf(
		"grep -F -- %s %s >/dev/null && sha256sum %s | awk '{print $1}'",
		framework.ShellQuote(s.dryRunMarker),
		framework.ShellQuote(manageJailStateDryRunFile),
		framework.ShellQuote(manageJailStateDryRunFile),
	)
	checksum, err := s.runtime.Jail().Run(ctx, command)
	if err != nil {
		return fmt.Errorf("verify dry-run left managed jail file %s unchanged: %w", manageJailStateDryRunFile, err)
	}
	if actual := strings.TrimSpace(checksum); actual != s.dryRunChecksum {
		return fmt.Errorf("expected managed jail file checksum %s after dry-run, got %s", s.dryRunChecksum, actual)
	}
	return nil
}

func (s *ActiveChecks) theManageJailStateDryRunActiveCheckStatusIsComplete(ctx context.Context) error {
	if s.dryRunJobName == "" {
		return fmt.Errorf("manage-jail-state dry-run Job is not captured")
	}
	checkName := s.activeCheckName(manageJailStateDryRunSuffix)
	return s.runtime.WaitFor(ctx, fmt.Sprintf("ActiveCheck %s Job %s complete", checkName, s.dryRunJobName), activeK8sJobTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		var check kubeobjects.ActiveCheck
		if err := s.kubectl.GetJSON(waitCtx, &check, "get", "activecheck", "-n", framework.SoperatorNamespace, checkName, "-o", "json"); err != nil {
			return false, err
		}
		status := check.Status.K8sJobsStatus
		if status.LastJobName != s.dryRunJobName {
			return false, nil
		}
		switch status.LastJobStatus {
		case kubeobjects.ActiveCheckK8sJobStatusComplete:
			return true, nil
		case kubeobjects.ActiveCheckK8sJobStatusFailed:
			return false, fmt.Errorf("ActiveCheck %s Job %s finished with status=%s", checkName, s.dryRunJobName, status.LastJobStatus)
		default:
			return false, nil
		}
	})
}

func (s *ActiveChecks) captureDryRunJobLogs(ctx context.Context) error {
	logs, err := s.runtime.Kubectl().RunWithDefaultRetry(ctx,
		"logs", "-n", framework.SoperatorNamespace, "job/"+s.dryRunJobName, "--all-containers=true",
	)
	if err != nil {
		return fmt.Errorf("capture manage-jail-state dry-run Job %s logs: %w", s.dryRunJobName, err)
	}
	s.dryRunLogs = logs
	if artifacts, ok := framework.ScenarioArtifacts(ctx); ok {
		artifactPath := filepath.Join(artifacts.RunnerDir, manageJailStateDryRunLogsFilename)
		if err := os.WriteFile(artifactPath, []byte(logs), 0o644); err != nil {
			return fmt.Errorf("write manage-jail-state dry-run logs to %s: %w", artifactPath, err)
		}
	}
	return nil
}

func (s *ActiveChecks) ensureDryRunAnsibleResult(ctx context.Context) error {
	if s.dryRunAnsibleResult.Parsed {
		return nil
	}
	if s.dryRunLogs == "" {
		if err := s.captureDryRunJobLogs(ctx); err != nil {
			return err
		}
	}
	result, err := parseAnsibleDryRunJSONL(s.dryRunLogs)
	if err != nil {
		return fmt.Errorf("parse manage-jail-state dry-run Ansible JSONL: %w", err)
	}
	s.dryRunAnsibleResult = result
	return nil
}

func (s *ActiveChecks) healthyGPUWorkersAreAvailableForActiveChecks(ctx context.Context) error {
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return err
	}
	if err := snapshot.RequireGPUWorkersUsable(); framework.IsInsufficientWorkers(err) {
		s.runtime.Logf("acceptance: no GPU workers configured for GPU ActiveCheck validation, skipping scenario")
		return godog.ErrSkip
	} else if err != nil {
		return fmt.Errorf("validate GPU workers for active checks: %w", err)
	}
	s.gpuWorkers = snapshot.GPUWorkers
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
	return framework.ClusterPrefixedName(s.info.SlurmClusterName, suffix)
}

func (s *ActiveChecks) createJobFromCronJob(ctx context.Context, cronJobName string) (string, error) {
	jobName := uniqueK8sJobName(strings.TrimPrefix(cronJobName, s.info.SlurmClusterName+"-"))
	if _, err := s.runtime.Kubectl().RunWithDefaultRetry(ctx,
		"create", "job", "-n", framework.SoperatorNamespace,
		"--from=cronjob/"+cronJobName,
		jobName,
	); err != nil {
		return "", fmt.Errorf("trigger CronJob %s as Job %s: %w", cronJobName, jobName, err)
	}
	s.runtime.Logf("active checks: triggered CronJob %s as Job %s", cronJobName, jobName)
	return jobName, nil
}

func (s *ActiveChecks) waitForK8sJobSlurmJobIDs(ctx context.Context, jobName string, timeout time.Duration) ([]string, error) {
	var latest k8sJobStatus
	err := s.runtime.WaitFor(ctx, fmt.Sprintf("Kubernetes Job %s Slurm job IDs", jobName), timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		var job k8sJobStatus
		if err := s.kubectl.GetJSON(waitCtx, &job, "get", "job", "-n", framework.SoperatorNamespace, jobName, "-o", "json"); err != nil {
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
	return s.runtime.WaitFor(ctx, fmt.Sprintf("Kubernetes Job %s complete", jobName), timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		var job k8sJobStatus
		if err := s.kubectl.GetJSON(waitCtx, &job, "get", "job", "-n", framework.SoperatorNamespace, jobName, "-o", "json"); err != nil {
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
	return s.runtime.WaitFor(ctx, fmt.Sprintf("ActiveCheck %s Slurm run %s complete", checkName, firstJobID), timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		var check kubeobjects.ActiveCheck
		if err := s.kubectl.GetJSON(waitCtx, &check, "get", "activecheck", "-n", framework.SoperatorNamespace, checkName, "-o", "json"); err != nil {
			return false, err
		}
		status := check.Status.SlurmJobsStatus
		if status.LastRunID != firstJobID {
			return false, nil
		}
		switch status.LastRunStatus {
		case kubeobjects.ActiveCheckSlurmRunStatusComplete:
			return true, nil
		case kubeobjects.ActiveCheckSlurmRunStatusFailed,
			kubeobjects.ActiveCheckSlurmRunStatusCancelled,
			kubeobjects.ActiveCheckSlurmRunStatusError,
			kubeobjects.ActiveCheckSlurmRunStatusSkipped:
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
	err := s.runtime.WaitFor(ctx, "GPU ActiveCheck Slurm jobs completed on all GPU workers", activeGPUCheckTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
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
	return assertGPUActiveCheckSlurmRecords(records, s.gpuCheckJobIDs, s.gpuWorkers)
}

func (s *ActiveChecks) waitForGPUActiveCheckOutputsPassingOnAllGPUWorkers(ctx context.Context) error {
	if len(s.gpuCheckJobIDs) == 0 {
		return fmt.Errorf("GPU ActiveCheck Slurm job IDs are not captured")
	}
	var records map[string]activeCheckSlurmJobRecord
	if err := s.runtime.WaitFor(ctx, "GPU ActiveCheck output files report PASS", activeSlurmOutputWait, activeSlurmOutputPoll, func(waitCtx context.Context) (bool, error) {
		next, err := s.gpuActiveCheckSlurmJobRecords(waitCtx)
		if err != nil {
			return false, err
		}
		if err := assertGPUActiveCheckSlurmRecordsTargetExpectedWorkers(next, s.gpuCheckJobIDs, s.gpuWorkers); err != nil {
			return false, err
		}
		for _, jobID := range s.gpuCheckJobIDs {
			record := next[jobID]
			out, exists, err := readWorkerFileIfExists(waitCtx, s.runtime, framework.WorkerInfo{Name: record.NodeList}, activeCheckSlurmJobOutputPath(record))
			if err != nil {
				return false, err
			}
			if !exists {
				return false, nil
			}
			if _, found := activeGPUCheckOutputStatus(out); !found {
				return false, nil
			}
			if err := assertActiveGPUCheckOutputPassing(out); err != nil {
				return false, err
			}
		}
		records = next
		return true, nil
	}); err != nil {
		return err
	}
	return assertGPUActiveCheckSlurmRecordsTargetExpectedWorkers(records, s.gpuCheckJobIDs, s.gpuWorkers)
}

func (s *ActiveChecks) gpuActiveCheckSlurmJobRecords(ctx context.Context) (map[string]activeCheckSlurmJobRecord, error) {
	out, err := s.runtime.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf(
		"sacct -j %s --noheader --parsable2 --format=JobID,JobName,State,ExitCode,NodeList 2>/dev/null || true",
		framework.ShellQuote(strings.Join(s.gpuCheckJobIDs, ",")),
	))
	if err != nil {
		return nil, fmt.Errorf("query GPU ActiveCheck Slurm jobs: %w", err)
	}
	return parseActiveCheckSlurmJobRecords(out), nil
}

func assertGPUActiveCheckSlurmRecords(records map[string]activeCheckSlurmJobRecord, jobIDs []string, expectedWorkers []framework.WorkerInfo) error {
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

func assertGPUActiveCheckSlurmRecordsTargetExpectedWorkers(records map[string]activeCheckSlurmJobRecord, jobIDs []string, expectedWorkers []framework.WorkerInfo) error {
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

type activeCheckCronJob struct {
	Spec struct {
		JobTemplate struct {
			Spec struct {
				Template struct {
					Spec struct {
						Containers []struct {
							Args []string `json:"args"`
						} `json:"containers"`
					} `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
		} `json:"jobTemplate"`
	} `json:"spec"`
}

type ansibleDryRunResult struct {
	Parsed             bool
	StatsFound         bool
	ManagedFileChanged bool
	Failures           int
	Unreachable        int
}

type ansibleJSONLEvent struct {
	Event string `json:"_event"`
	Task  struct {
		Name string `json:"name"`
	} `json:"task"`
	Hosts map[string]struct {
		Changed bool `json:"changed"`
	} `json:"hosts"`
	Stats map[string]struct {
		Failures    int `json:"failures"`
		Unreachable int `json:"unreachable"`
	} `json:"stats"`
}

func parseAnsibleDryRunJSONL(output string) (ansibleDryRunResult, error) {
	var result ansibleDryRunResult
	for lineNumber, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}

		var event ansibleJSONLEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return ansibleDryRunResult{}, fmt.Errorf("decode JSONL event on line %d: %w", lineNumber+1, err)
		}
		result.Parsed = true
		switch event.Event {
		case "v2_runner_on_ok":
			if !strings.HasSuffix(event.Task.Name, manageJailStateDryRunTask) {
				continue
			}
			for _, host := range event.Hosts {
				result.ManagedFileChanged = result.ManagedFileChanged || host.Changed
			}
		case "v2_playbook_on_stats":
			result.StatsFound = true
			result.Failures = 0
			result.Unreachable = 0
			for _, stats := range event.Stats {
				result.Failures += stats.Failures
				result.Unreachable += stats.Unreachable
			}
		}
	}
	if !result.Parsed {
		return ansibleDryRunResult{}, fmt.Errorf("output does not contain JSONL events")
	}
	return result, nil
}

type activeCheckSlurmJobRecord struct {
	ID       string
	JobName  string
	State    string
	ExitCode string
	NodeList string
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
