package sharedsteps

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type workerDiagnosticsRuntime struct {
	framework.Runtime
	jail, worker framework.CommandScope
	waitErr      error
}

func (r *workerDiagnosticsRuntime) Jail() framework.CommandScope { return r.jail }
func (r *workerDiagnosticsRuntime) Worker(framework.WorkerInfo) framework.CommandScope {
	return r.worker
}
func (*workerDiagnosticsRuntime) Logf(string, ...any) {}
func (r *workerDiagnosticsRuntime) WaitFor(context.Context, string, time.Duration, time.Duration, func(context.Context) (bool, error)) error {
	return r.waitErr
}

func TestFailureDiagnosticsSurviveExpiredContextBeforeCleanup(t *testing.T) {
	for _, family := range []string{"nvidia", "enroot"} {
		t.Run(family, func(t *testing.T) {
			failure := errors.New("original operation timed out")
			var events []string
			runtime := &workerDiagnosticsRuntime{waitErr: failure}
			runtime.jail = framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
				if strings.HasPrefix(command, "cd ") {
					return "report generation output", failure
				}
				if strings.HasPrefix(command, "scancel ") {
					events = append(events, "cleanup")
				}
				return "", nil
			})
			runtime.worker = framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
				if strings.HasPrefix(command, "rm ") {
					events = append(events, "cleanup")
					return "", nil
				}
				require.NoError(t, ctx.Err())
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				assert.Positive(t, time.Until(deadline))
				assert.LessOrEqual(t, time.Until(deadline), 12*time.Second)
				events = append(events, "snapshot")
				// Check shell syntax with paths that require quoting, without executing probes.
				check := exec.CommandContext(ctx, "bash", "-n")
				check.Stdin = strings.NewReader(command)
				output, err := check.CombinedOutput()
				require.NoError(t, err, "%s", output)
				if family == "nvidia" {
					assert.Contains(t, command, "16777216")
					assert.Contains(t, command, "/run/nvidia/driver/nvidia-bug-report.log.gz")
					assert.Contains(t, command, "partial.log.gz")
				} else {
					assert.Contains(t, command, enrootSquashRoot)
					assert.Contains(t, command, "tail -v -n 200")
				}
				return "partial evidence\n" + strings.Repeat("x", workerDiagnosticsLimit), context.DeadlineExceeded
			})
			ctx, err := framework.NewArtifactsManager(t.TempDir()+"/a'b", runtime.Jail()).PrepareScenario(
				t.Context(), "suite", "feature", "scenario", "id")
			require.NoError(t, err)
			artifacts, ok := framework.ScenarioArtifacts(ctx)
			require.True(t, ok)
			ctx, cancel := context.WithCancel(ctx)
			cancel()
			worker := framework.WorkerInfo{Name: "worker-1"}
			worker.SlurmNode.InstanceID = "instance-1"
			var run func(context.Context) error
			var cleanup func(context.Context)
			count := 1
			if family == "nvidia" {
				steps := &SoperatorUtils{runtime: runtime, worker: worker}
				run, cleanup = steps.downloadNVIDIABugReport, steps.CleanupAndReset
			} else {
				count = 2
				steps := &EnrootContainers{runtime: runtime, slurm: framework.NewSlurmClient(runtime),
					workers: []framework.WorkerInfo{worker, {Name: "worker-2"}}, connectionWorker: worker,
					job: framework.SbatchJob{ID: "8", StdoutPath: "/artifacts/a'b.out", StderrPath: "/artifacts/job.err"}}
				run, cleanup = steps.anEnrootImageArtifactIsPresentOnAWorker, steps.CleanupAndReset
			}
			assert.ErrorIs(t, run(ctx), failure)
			assert.Equal(t, count, len(events))
			for _, event := range events {
				assert.Equal(t, "snapshot", event)
			}
			cleanup(ctx)
			assert.Equal(t, "cleanup", events[len(events)-1])
			data, err := os.ReadFile(filepath.Join(artifacts.RunnerDir, "worker-1-diagnostics.txt"))
			require.NoError(t, err)
			assert.Contains(t, string(data), "partial evidence")
			assert.Contains(t, string(data), "[Diagnostics truncated]")
			assert.Contains(t, string(data), "context deadline exceeded")
			assert.Less(t, len(data), workerDiagnosticsLimit+512)
			if family == "nvidia" {
				data, err := os.ReadFile(filepath.Join(artifacts.RunnerDir, "nvidia-report-collection.txt"))
				require.NoError(t, err)
				assert.Contains(t, string(data), "report generation output")
				assert.Contains(t, string(data), failure.Error())
			}
		})
	}
}

func TestWorkerDiagnosticsWithoutArtifactDirectory(t *testing.T) {
	collectWorkerDiagnostics(t.Context(), nil, []framework.WorkerInfo{{Name: "worker-1"}}, "")
}

func TestNVIDIAReportSnapshotPreservesBinaryDataBeforeRemoval(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "report's artifacts")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	worker := framework.WorkerInfo{Name: "worker-1"}
	command := nvidiaFailureDiagnostics(framework.ScenarioArtifactPaths{JailDir: dir}, worker)
	source := filepath.Join(t.TempDir(), "source's report.gz")
	data := []byte{0x1f, 0x8b, 0, 0xff, '\n'}
	require.NoError(t, os.WriteFile(source, data, 0o600))
	command = strings.ReplaceAll(command, framework.ShellQuote(soperatorUtilsRemoteReportPath), framework.ShellQuote(source))
	missing := filepath.Join(t.TempDir(), "missing.gz")
	command = strings.ReplaceAll(command, framework.ShellQuote("/run/nvidia/driver/nvidia-bug-report.log.gz"), framework.ShellQuote(missing))
	// The probes use GNU timeout on workers; run the copy locally without that dependency.
	copyCommand := exec.CommandContext(t.Context(), "bash", "-c", "timeout() { shift 3; \"$@\"; }\n"+command)
	output, err := copyCommand.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.NoError(t, os.Remove(source))
	preserved, err := os.ReadFile(filepath.Join(dir, "worker-1-0-nvidia-bug-report.partial.log.gz"))
	require.NoError(t, err)
	assert.Equal(t, data, preserved)
	_, err = os.Stat(filepath.Join(dir, "worker-1-1-nvidia-bug-report.partial.log.gz"))
	assert.True(t, os.IsNotExist(err))
}
