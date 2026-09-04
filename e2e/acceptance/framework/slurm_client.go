package framework

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// AcceptanceJobOutputDir is where every sbatch submitted via SubmitBatch
// writes its stdout / stderr. The directory lives inside the jail on a shared
// volume captured by the CI jail artifact, so job output survives past the run.
const AcceptanceJobOutputDir = "/opt/soperator-outputs/shared/acceptance"

type SlurmClient struct {
	runtime Runtime
}

func NewSlurmClient(runtime Runtime) *SlurmClient {
	return &SlurmClient{runtime: runtime}
}

// SbatchOptions describes a Slurm batch submission from an acceptance test.
// SubmitBatch adds -o/-e pointing inside OutputDir, which defaults to
// AcceptanceJobOutputDir.
type SbatchOptions struct {
	JobName      string   // required; used in output filename
	Nodes        int      // -N (omitted if 0)
	Nodelist     []string // --nodelist (omitted if empty)
	GPUsPerNode  int      // --gpus-per-node (omitted if 0)
	TasksPerNode int      // --ntasks-per-node (omitted if 0)
	ExtraFlags   []string // verbatim flags appended before --wrap
	Wrap         string   // --wrap body
	RunAsUser    string   // submit through sudo -iu (omitted if empty)
	OutputDir    string   // defaults to AcceptanceJobOutputDir
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
	outputDir := strings.TrimSpace(opts.OutputDir)
	if outputDir == "" {
		outputDir = AcceptanceJobOutputDir
	}
	runAsUser := strings.TrimSpace(opts.RunAsUser)

	var args []string
	args = append(args, "--parsable")
	args = append(args, fmt.Sprintf("--job-name=%s", ShellQuote(jobName)))
	args = append(args, fmt.Sprintf("-o %s", ShellQuote(outputDir+"/%x-%j.out")))
	args = append(args, fmt.Sprintf("-e %s", ShellQuote(outputDir+"/%x-%j.err")))
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

	mkdirCommand := fmt.Sprintf("mkdir -p %s", ShellQuote(outputDir))
	sbatchCommand := fmt.Sprintf("sbatch %s", strings.Join(args, " "))
	if runAsUser != "" {
		quotedUser := ShellQuote(runAsUser)
		mkdirCommand = fmt.Sprintf("sudo -iu %s -- mkdir -p %s", quotedUser, ShellQuote(outputDir))
		sbatchCommand = fmt.Sprintf("sudo -iu %s -- sbatch %s", quotedUser, strings.Join(args, " "))
	}
	command := mkdirCommand + " && " + sbatchCommand

	// TODO: Add safe retries for sbatch without creating duplicate jobs.
	out, err := s.runtime.Jail().Run(ctx, command)
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
		StdoutPath: fmt.Sprintf("%s/%s-%s.out", outputDir, jobName, jobID),
		StderrPath: fmt.Sprintf("%s/%s-%s.err", outputDir, jobName, jobID),
	}, nil
}

func (s *SlurmClient) WaitForJobRunning(ctx context.Context, jobID string, timeout time.Duration) error {
	return s.runtime.WaitFor(ctx, fmt.Sprintf("job %s running", jobID), timeout, DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		status, err := s.runtime.Jail().RunWithDefaultRetry(waitCtx, fmt.Sprintf("squeue -h -j %s -o '%%T'", ShellQuote(jobID)))
		if err != nil {
			return false, err
		}
		return strings.Contains(status, "RUNNING"), nil
	})
}

func (s *SlurmClient) WaitForJobGone(ctx context.Context, jobID string, timeout time.Duration) error {
	return s.runtime.WaitFor(ctx, fmt.Sprintf("job %s gone from queue", jobID), timeout, DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		status, err := s.runtime.Jail().RunWithDefaultRetry(waitCtx, fmt.Sprintf("squeue -h -j %s -o '%%T'", ShellQuote(jobID)))
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

	if _, err := s.runtime.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf("scancel %s", ShellQuote(jobID))); err != nil {
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

func isMissingJobError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "invalid job id") || strings.Contains(message, "does not exist")
}
