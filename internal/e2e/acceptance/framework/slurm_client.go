package framework

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// AcceptanceJobOutputDir is where every sbatch submitted via SubmitBatch
// writes its stdout / stderr. The directory lives inside the jail on a shared
// volume captured by the CI `jail` artifact (see .github/workflows/e2e_test.yml
// "Collect Jail Files" step), so job output survives past the run.
const AcceptanceJobOutputDir = "/opt/soperator-outputs/acceptance"

type SlurmClient struct {
	exec Exec
}

func NewSlurmClient(exec Exec) *SlurmClient {
	return &SlurmClient{exec: exec}
}

// SbatchOptions describes a Slurm batch submission from an acceptance test.
// SubmitBatch always adds -o/-e pointing inside AcceptanceJobOutputDir so the
// job log ends up in the CI artifact; callers don't set output flags.
type SbatchOptions struct {
	JobName      string   // required; used in output filename
	Nodes        int      // -N (omitted if 0)
	Nodelist     []string // --nodelist (omitted if empty)
	GPUsPerNode  int      // --gpus-per-node (omitted if 0)
	TasksPerNode int      // --ntasks-per-node (omitted if 0)
	ExtraFlags   []string // verbatim flags appended before --wrap
	Wrap         string   // --wrap body
}

// SbatchJob is the handle returned by SubmitBatch. StdoutPath and StderrPath
// are fully resolved filesystem paths (Slurm's %x/%j patterns already expanded
// in Go), useful for log‑tailing in failure paths.
type SbatchJob struct {
	ID         string
	JobName    string
	StdoutPath string
	StderrPath string
}

type SlurmJobInfo struct {
	ID          string
	QueueState  string
	SacctDump   string
	SacctFound  bool
	SacctState  string
	SacctExit   string
	SacctReason string
}

// IsZero reports whether j carries no submission — e.g. after a reset. Useful
// as a guard before annotating errors with a missing job's logs.
func (j SbatchJob) IsZero() bool {
	return strings.TrimSpace(j.ID) == ""
}

// SubmitBatch assembles an sbatch command from opts, ensures the output
// directory exists, submits it from the jail, and returns an SbatchJob with
// the parsed job id and the resolved stdout / stderr paths.
func (s *SlurmClient) SubmitBatch(ctx context.Context, opts SbatchOptions) (SbatchJob, error) {
	jobName := strings.TrimSpace(opts.JobName)
	if jobName == "" {
		return SbatchJob{}, fmt.Errorf("sbatch: job name is required")
	}
	if strings.TrimSpace(opts.Wrap) == "" {
		return SbatchJob{}, fmt.Errorf("sbatch: wrap body is required")
	}

	var args []string
	args = append(args, "--parsable")
	args = append(args, fmt.Sprintf("--job-name=%s", jobName))
	args = append(args, fmt.Sprintf("-o %s/%%x-%%j.out", AcceptanceJobOutputDir))
	args = append(args, fmt.Sprintf("-e %s/%%x-%%j.err", AcceptanceJobOutputDir))
	if opts.Nodes > 0 {
		args = append(args, fmt.Sprintf("-N %d", opts.Nodes))
	}
	if len(opts.Nodelist) > 0 {
		args = append(args, fmt.Sprintf("--nodelist=%s", ShellQuote(strings.Join(opts.Nodelist, ","))))
	}
	if opts.GPUsPerNode > 0 {
		args = append(args, fmt.Sprintf("--gpus-per-node=%d", opts.GPUsPerNode))
	}
	if opts.TasksPerNode > 0 {
		args = append(args, fmt.Sprintf("--ntasks-per-node=%d", opts.TasksPerNode))
	}
	for _, flag := range opts.ExtraFlags {
		args = append(args, flag)
	}
	args = append(args, fmt.Sprintf("--wrap=%s", ShellQuote(opts.Wrap)))

	command := fmt.Sprintf("mkdir -p %s && sbatch %s",
		ShellQuote(AcceptanceJobOutputDir),
		strings.Join(args, " "),
	)

	// TODO: Add safe retries for sbatch without creating duplicate jobs.
	out, err := s.exec.Jail().Run(ctx, command)
	if err != nil {
		return SbatchJob{}, fmt.Errorf("submit sbatch job %q: %w", jobName, err)
	}
	jobID, err := ParseSbatchJobID(out)
	if err != nil {
		return SbatchJob{}, fmt.Errorf("parse sbatch job id for %q: %w", jobName, err)
	}
	return SbatchJob{
		ID:         jobID,
		JobName:    jobName,
		StdoutPath: fmt.Sprintf("%s/%s-%s.out", AcceptanceJobOutputDir, jobName, jobID),
		StderrPath: fmt.Sprintf("%s/%s-%s.err", AcceptanceJobOutputDir, jobName, jobID),
	}, nil
}

// JobInfo returns the current squeue state and a parsed sacct record for the
// top-level job when sacct has already recorded one.
func (s *SlurmClient) JobInfo(ctx context.Context, jobID string) (SlurmJobInfo, error) {
	id := strings.TrimSpace(jobID)
	if id == "" {
		return SlurmJobInfo{}, fmt.Errorf("job id is empty")
	}
	rawState, queueErr := s.exec.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf("squeue -h -j %s -o '%%T' 2>/dev/null || true", ShellQuote(id)))
	if queueErr != nil {
		return SlurmJobInfo{}, fmt.Errorf("query squeue for job %s: %w", id, queueErr)
	}
	info := SlurmJobInfo{
		ID:         id,
		QueueState: strings.TrimSpace(rawState),
	}
	s.addBestEffortSacctInfo(ctx, &info)
	return info, nil
}

func (s *SlurmClient) addBestEffortSacctInfo(ctx context.Context, info *SlurmJobInfo) {
	rawDump, err := s.exec.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf(
		"sacct -j %s --noheader --parsable2 --format=JobID,State,ExitCode,Reason,Start,End 2>/dev/null || true",
		ShellQuote(info.ID),
	))
	if err != nil {
		return
	}
	info.SacctDump = strings.TrimSpace(rawDump)
	info.SacctState, info.SacctExit, info.SacctReason, info.SacctFound = parseSacctJob(info.SacctDump, info.ID)
}

// AssertJobRunning returns an error if jobID is not currently in RUNNING state.
// Strict equality with "RUNNING": once a scenario has cleared WaitForJobRunning,
// transitions back to PENDING / COMPLETING / etc. are unexpected and worth flagging.
// The returned error carries the observed state; sacct dump and log tails are
// expected to come from a call‑site wrapper such as AnnotateWithJobLog.
func (s *SlurmClient) AssertJobRunning(ctx context.Context, jobID string) error {
	info, err := s.JobInfo(ctx, jobID)
	if err != nil {
		return err
	}
	if info.QueueState != "RUNNING" {
		return fmt.Errorf("expected job %s to be RUNNING, got state=%q", strings.TrimSpace(jobID), info.QueueState)
	}
	return nil
}

func (i SlurmJobInfo) IsAlive() bool {
	if strings.TrimSpace(i.QueueState) != "" {
		return IsJobAliveState(i.QueueState)
	}
	if i.SacctFound {
		return IsJobAliveState(i.SacctState)
	}
	return false
}

func (i SlurmJobInfo) CompletedSuccessfully() bool {
	return i.SacctFound && i.SacctState == "COMPLETED" && i.SacctExit == "0:0"
}

func parseSacctJob(dump, jobID string) (state, exitCode, reason string, found bool) {
	for _, line := range strings.Split(dump, "\n") {
		fields := strings.Split(line, "|")
		if len(fields) < 3 {
			continue
		}
		if strings.TrimSpace(fields[0]) != jobID {
			continue
		}
		state = strings.TrimSpace(fields[1])
		exitCode = strings.TrimSpace(fields[2])
		if len(fields) > 3 {
			reason = strings.TrimSpace(fields[3])
		}
		return state, exitCode, reason, true
	}
	return "", "", "", false
}

// IsJobAliveState reports whether state represents a job that Slurm still considers live
// (scheduling, running, or finalizing). An empty state — squeue no longer lists the job, meaning
// it has finished and been dropped from the active queue — is treated as not alive.
func IsJobAliveState(state string) bool {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "PENDING", "CONFIGURING", "RUNNING", "COMPLETING", "SUSPENDED", "RESIZING", "REQUEUED", "REQUEUE_HOLD", "REQUEUE_FED", "SIGNALING":
		return true
	default:
		return false
	}
}

func (s *SlurmClient) AnyWorkers(count int) ([]WorkerRef, error) {
	return pickAnyWorkerRefs(s.exec.AvailableWorkers(), count, "workers")
}

func (s *SlurmClient) AnyGPUWorkers(count int) ([]WorkerRef, error) {
	return pickAnyWorkerRefs(s.exec.AvailableGPUWorkers(), count, "GPU workers")
}

func (s *SlurmClient) WaitForJobRunning(ctx context.Context, jobID string, timeout time.Duration) error {
	return s.exec.WaitFor(ctx, fmt.Sprintf("job %s running", jobID), timeout, DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		status, err := s.exec.Jail().RunWithDefaultRetry(waitCtx, fmt.Sprintf("squeue -h -j %s -o '%%T'", ShellQuote(jobID)))
		if err != nil {
			return false, err
		}
		return strings.Contains(status, "RUNNING"), nil
	})
}

func (s *SlurmClient) WaitForJobGone(ctx context.Context, jobID string, timeout time.Duration) error {
	return s.exec.WaitFor(ctx, fmt.Sprintf("job %s gone from queue", jobID), timeout, DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		status, err := s.exec.Jail().RunWithDefaultRetry(waitCtx, fmt.Sprintf("squeue -h -j %s -o '%%T'", ShellQuote(jobID)))
		if err != nil {
			// Some Slurm deployments exit non‑zero with "Invalid job id specified"
			// once a job has left the active queue. Treat that as "gone" so this
			// wait doesn't burn its timeout on an already‑finished job.
			if isMissingJobError(err) {
				return true, nil
			}
			return false, err
		}
		return strings.TrimSpace(status) == "", nil
	})
}

func (s *SlurmClient) CancelJob(ctx context.Context, jobID string, waitTimeout time.Duration) error {
	if strings.TrimSpace(jobID) == "" {
		return nil
	}

	if _, err := s.exec.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf("scancel %s", ShellQuote(jobID))); err != nil {
		if !isMissingJobError(err) {
			return fmt.Errorf("scancel job %s: %w", jobID, err)
		}
	}

	if waitTimeout <= 0 {
		return nil
	}
	if err := s.WaitForJobGone(ctx, jobID, waitTimeout); err != nil {
		return fmt.Errorf("wait for job %s to finish: %w", jobID, err)
	}
	return nil
}

func pickAnyWorkerRefs(pool []WorkerRef, count int, label string) ([]WorkerRef, error) {
	if count < 1 {
		return nil, fmt.Errorf("invalid %s count %d", label, count)
	}
	if len(pool) < count {
		return nil, fmt.Errorf("found %d %s, need %d", len(pool), label, count)
	}

	indices := rand.Perm(len(pool))[:count]
	out := make([]WorkerRef, 0, count)
	for _, i := range indices {
		out = append(out, pool[i])
	}
	return out, nil
}

func isMissingJobError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "invalid job id") || strings.Contains(message, "does not exist")
}
