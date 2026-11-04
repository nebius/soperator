package framework

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type SlurmJobInfo struct {
	ID          string
	QueueState  string
	SacctDump   string
	SacctFound  bool
	SacctState  string
	SacctExit   string
	SacctReason string
	Restarts    *int // nil means unknown; pointer to 0 means no restarts.
}

// JobReleaseGate waits for a release file and exits with status 124 if the
// deadline expires, allowing a test to hold a job until its observed event.
func JobReleaseGate(releasePath string, timeout time.Duration) string {
	seconds := strconv.FormatFloat(timeout.Seconds(), 'f', -1, 64)
	return fmt.Sprintf("timeout %ss sh -c %s", seconds, ShellQuote(
		"while [ ! -f "+ShellQuote(releasePath)+" ]; do sleep 1; done"))
}

// JobInfo returns the current squeue state and a parsed sacct record for the
// top-level job when sacct has already recorded one.
func (s *SlurmClient) JobInfo(ctx context.Context, jobID string) (SlurmJobInfo, error) {
	info, err := s.JobStatus(ctx, jobID)
	if err != nil {
		return info, err
	}
	s.addBestEffortJobRestartCount(ctx, &info)
	return info, nil
}

// JobStatus reads queue and accounting states without querying restart metadata.
func (s *SlurmClient) JobStatus(ctx context.Context, jobID string) (SlurmJobInfo, error) {
	id := strings.TrimSpace(jobID)
	if id == "" {
		return SlurmJobInfo{}, fmt.Errorf("job id is empty")
	}
	rawState, queueErr := s.runtime.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf("squeue -h -j %s -o '%%T' 2>/dev/null || true", ShellQuote(id)))
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
	rawDump, err := s.runtime.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf(
		"sacct -j %s --noheader --parsable2 --format=JobID,State,ExitCode,Reason,Start,End 2>/dev/null || true",
		ShellQuote(info.ID),
	))
	if err != nil {
		return
	}
	info.SacctDump = strings.TrimSpace(rawDump)
	info.SacctState, info.SacctExit, info.SacctReason, info.SacctFound = parseSacctJob(info.SacctDump, info.ID)
}

func (s *SlurmClient) addBestEffortJobRestartCount(ctx context.Context, info *SlurmJobInfo) {
	out, err := s.runtime.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf(
		"scontrol -o show job %s",
		ShellQuote(info.ID),
	))
	if err != nil {
		return
	}

	for _, field := range strings.Fields(out) {
		value, found := strings.CutPrefix(field, "Restarts=")
		if !found {
			continue
		}

		count, err := strconv.Atoi(value)
		if err != nil || count < 0 {
			return
		}
		info.Restarts = &count
	}

	return
}

// AssertJobRunning returns an error if jobID is not currently in RUNNING state.
// Strict equality with "RUNNING": once a scenario has cleared WaitForJobRunning,
// transitions back to PENDING / COMPLETING / etc. are unexpected and worth flagging.
// The returned error carries the observed state; sacct dump and log tails are
// expected to come from a call-site wrapper such as AnnotateWithJobLog.
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

func (s *SlurmClient) AssertJobRunningWithoutRestarts(ctx context.Context, jobID string) error {
	info, err := s.JobInfo(ctx, jobID)
	if err != nil {
		return err
	}
	if info.QueueState != "RUNNING" || !info.HasNoRestarts() {
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

func (i SlurmJobInfo) HasNoRestarts() bool {
	return i.Restarts != nil && *i.Restarts == 0
}

func (i SlurmJobInfo) CompletedSuccessfullyWithoutRestarts() bool {
	return i.CompletedSuccessfully() && i.HasNoRestarts()
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
// (scheduling, running, or finalizing). An empty state means squeue no longer lists the job,
// so it has finished and been dropped from the active queue.
func IsJobAliveState(state string) bool {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "PENDING", "CONFIGURING", "RUNNING", "COMPLETING", "SUSPENDED", "RESIZING", "REQUEUED", "REQUEUE_HOLD", "REQUEUE_FED", "SIGNALING":
		return true
	default:
		return false
	}
}
