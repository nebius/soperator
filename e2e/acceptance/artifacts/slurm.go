package artifacts

import (
	"context"
	"errors"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type slurmCollector struct {
	controller framework.CommandScope
}

// NewSlurmCollector creates a collector for Slurm controller state.
func NewSlurmCollector(controller framework.CommandScope) Collector {
	return &slurmCollector{controller: controller}
}

func (c *slurmCollector) Name() string { return "slurm" }

func (c *slurmCollector) Collect(ctx context.Context, destination string) error {
	commands := []struct {
		filename string
		command  string
	}{
		{"sinfo.txt", "sinfo -N --long"},
		{"squeue.txt", "squeue --all --long"},
		{"sdiag.txt", "sdiag"},
		{"sacct.txt", "sacct --allusers --starttime=now-6hours --format=JobID,JobName,Partition,Account,AllocCPUS,State,ExitCode"},
		{"sacct-parsable.txt", "sacct --parsable2 --allusers --starttime=now-6hours --format=JobID,JobName,Partition,Account,AllocCPUS,State,ExitCode,NodeList,Reason,StdOut,StdErr,WorkDir"},
	}
	var failures []error
	for _, command := range commands {
		if err := collectCommand(ctx, destination, command.filename, c.controller, command.command); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
