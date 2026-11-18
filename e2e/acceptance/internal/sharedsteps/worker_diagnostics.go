package sharedsteps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const workerDiagnosticsLimit = 256 * 1024

// Collect before destructive cleanup, even when the failing step's context expired.
func collectWorkerDiagnostics(ctx context.Context, runtime framework.Runtime, workers []framework.WorkerInfo, extra string) {
	artifacts, ok := framework.ScenarioArtifacts(ctx)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	for _, worker := range workers {
		workerCtx, stop := context.WithTimeout(ctx, 12*time.Second)
		command := "sudo timeout -k 1s 10s bash -o pipefail -c " + framework.ShellQuote(`
umask 022
date -u --iso-8601=seconds
printf '\n== Processes (PID, parent, elapsed seconds, state, wait channel, command) ==\n'
timeout -k 1s 2s ps -e -ww -o pid,ppid,etimes,stat,wchan:32,args --forest 2>&1 | head -c 65536 || printf '\nProcess snapshot status=%s\n' "$?"
`+extra+`
printf '\n== Locks ==\n'
timeout -k 1s 2s lslocks -o PID,COMMAND,TYPE,MODE,PATH 2>&1 | head -c 65536 || printf '\nLock snapshot status=%s\n' "$?"
printf '\n== TCP connections ==\n'
timeout -k 1s 2s ss -tpna 2>&1 | head -c 65536 || printf '\nConnection snapshot status=%s\n' "$?"
`) + " 2>&1"
		output, err := runtime.Worker(worker).Run(workerCtx, command)
		stop()
		if len(output) > workerDiagnosticsLimit {
			output = output[:workerDiagnosticsLimit] + "\n[Diagnostics truncated]\n"
		}
		output = fmt.Sprintf("worker=%s instance=%s\n%s\ncollection_error=%v\n", worker.Name, worker.SlurmNode.InstanceID, output, err)
		if err != nil {
			runtime.Logf("Collect worker diagnostics for %s: %v", worker.Name, err)
		}
		if err := artifacts.WriteRunnerFile(worker.Name+"-diagnostics.txt", output); err != nil {
			runtime.Logf("Write worker diagnostics for %s: %v", worker.Name, err)
		}
	}
}

func enrootFailureDiagnostics(job framework.SbatchJob) string {
	var command strings.Builder
	fmt.Fprintf(&command, "\nprintf '\\n== Enroot cache files (mtime, bytes, path), job %%s ==\\n' %s\n", framework.ShellQuote(job.ID))
	fmt.Fprintf(&command, "timeout -k 1s 2s find %s -maxdepth 2 -printf '%%T+ %%s %%p\\n' 2>&1 | head -c 65536 || printf '\\nCache snapshot status=%%s\\n' \"$?\"\n", framework.ShellQuote(enrootSquashRoot))
	for _, file := range []string{job.StdoutPath, job.StderrPath} {
		if file != "" {
			fmt.Fprintf(&command, "timeout -k 1s 2s tail -v -n 200 -- %s 2>&1 | head -c 32768 || printf '\\nJob log snapshot status=%%s\\n' \"$?\"\n", framework.ShellQuote(file))
		}
	}
	return command.String()
}

func nvidiaFailureDiagnostics(artifacts framework.ScenarioArtifactPaths, worker framework.WorkerInfo) string {
	var command strings.Builder
	for i, file := range []string{soperatorUtilsRemoteReportPath, "/run/nvidia/driver/nvidia-bug-report.log.gz"} {
		destination := fmt.Sprintf("%s/%s-%d-nvidia-bug-report.partial.log.gz", artifacts.JailDir, worker.Name, i)
		fmt.Fprintf(&command, "\nprintf 'Report snapshot (at most 16 MiB): %%s\\n' %s\n", framework.ShellQuote(destination))
		fmt.Fprintf(&command, "\nprintf '\\n== NVIDIA report ==\\n'\nstat -- %s 2>&1\n", framework.ShellQuote(file))
		fmt.Fprintf(&command, "if test -f %s; then timeout -k 1s 2s head -c 16777216 -- %s > %s && chmod 644 -- %s || printf 'Report snapshot status=%%s\\n' \"$?\"; fi\n",
			framework.ShellQuote(file), framework.ShellQuote(file), framework.ShellQuote(destination), framework.ShellQuote(destination))
	}
	return command.String()
}
