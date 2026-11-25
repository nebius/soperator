package sharedsteps

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestLoggedCheckOutputPathsNormalizesJailPrefix(t *testing.T) {
	output := `
[2026-01-01 00:00:00.000 UTC] INFO: Running check one (true), logging to /mnt/jail/opt/soperator-outputs/local/slurm_scripts/worker-0.one.prolog.out
[2026-01-01 00:00:01.000 UTC] INFO: Running check two (true), logging to /opt/soperator-outputs/local/slurm_scripts/worker-0.two.prolog.out
[2026-01-01 00:00:02.000 UTC] INFO: Running check one (true), logging to /mnt/jail/opt/soperator-outputs/local/slurm_scripts/worker-0.one.prolog.out
`
	assert.Equal(t, []string{
		"/opt/soperator-outputs/local/slurm_scripts/worker-0.one.prolog.out",
		"/opt/soperator-outputs/local/slurm_scripts/worker-0.two.prolog.out",
	}, loggedCheckOutputPaths(output))
}

func TestVersionAwareCheckOutputPaths(t *testing.T) {
	assert.Equal(t,
		"/opt/soperator-outputs/slurm_scripts/worker-0.check_runner.prolog.out",
		checkRunnerOutputPath("4.0.2", "worker-0", "prolog"),
	)
	assert.Equal(t,
		"/opt/soperator-outputs/local/slurm_scripts/worker-0.check_runner.prolog.out",
		checkRunnerOutputPath("5.0.0", "worker-0", "prolog"),
	)
	assert.Equal(t,
		"/opt/soperator-outputs/slurm_scripts/worker-0.gpu_health_check.epilog.out",
		gpuHealthCheckOutputPath("4.0.2", "worker-0", "epilog"),
	)
	assert.Equal(t,
		"/opt/soperator-outputs/local/slurm_scripts/worker-0.gpu_health_check.epilog.out",
		gpuHealthCheckOutputPath("5.0.0", "worker-0", "epilog"),
	)
	assert.Equal(t,
		"/opt/soperator-outputs/local/slurm_scripts/worker-0.nvidia_libraries.hc_program.out",
		checkLogPath("5.0.0", "worker-0", "nvidia_libraries", "hc_program"),
	)
}

func TestVersionAwareHealthCheckerStdoutOutputDir(t *testing.T) {
	assert.Equal(t, "/opt/soperator-outputs/health_checker_cmd_stdout", healthCheckerStdoutOutputDir("4.0.2"))
	assert.Equal(t, "/opt/soperator-outputs/local/health_checker_cmd_stdout", healthCheckerStdoutOutputDir("5.0.0"))
	assert.Equal(t, "/opt/soperator-outputs/local/health_checker_cmd_stdout", healthCheckerStdoutOutputDir(""))
}

func TestAssertCheckRunnerHealthy(t *testing.T) {
	healthy := `
	[2026-01-01 00:00:00.000 UTC] INFO: Started
	[2026-01-01 00:00:01.000 UTC] INFO: Check job_tmpfs_delete: OK
	[2026-01-01 00:00:02.000 UTC] INFO: Finished in 1.000 seconds
	`
	require.NoError(t, assertCheckRunnerHealthy(healthy))

	failed := strings.Replace(healthy, "Check job_tmpfs_delete: OK", "Check job_tmpfs_delete: FAIL (boom)", 1)
	require.Error(t, assertCheckRunnerHealthy(failed))
}

func TestCheckRunnerOutputMatchesJob(t *testing.T) {
	output := `
[2026-01-01 00:00:00.000 UTC] INFO: Environment SLURM_JOB_ID="23"
[2026-01-01 00:00:00.001 UTC] INFO: Environment SLURM_JOBID="23"
`
	assert.True(t, checkRunnerOutputMatchesJob(output, "23"))
	assert.False(t, checkRunnerOutputMatchesJob(output, "16"))
}

func TestParseHealthCheckReports(t *testing.T) {
	output := `
noise
{"status":"PASS","meta":{"run_id":"run-1"}}
{"status":"PASS","meta":{"run_id":"run-2"}}
`
	reports, err := parseHealthCheckReports(output)
	require.NoError(t, err)
	runIDs, err := assertHealthCheckReportsPassing(reports)
	require.NoError(t, err)
	assert.Equal(t, []string{"run-1", "run-2"}, runIDs)
}

func TestHealthCheckReportsMatchJob(t *testing.T) {
	reports := []healthCheckReport{
		{
			Status: "PASS",
			Meta: map[string]any{
				"run_id":      "run-1",
				"environment": "SLURM_JOB_USER=root SLURM_JOB_ID=23 SLURM_JOBID=23",
			},
		},
	}
	assert.True(t, healthCheckReportsMatchJob(reports, "23"))
	assert.False(t, healthCheckReportsMatchJob(reports, "16"))
	assert.False(t, healthCheckReportsMatchJob(reports, "2"))
}

func TestUniqueK8sJobName(t *testing.T) {
	name := uniqueK8sJobName("gpu-checks")
	assert.LessOrEqual(t, len(name), 63)
	assert.Contains(t, name, "acceptance-gpu-checks-")
}

func TestAssertGPUActiveCheckSlurmRecords(t *testing.T) {
	records := parseActiveCheckSlurmJobRecords(`
101|soperator-gpu-checks|COMPLETED|0:0|worker-0
101.batch|batch|COMPLETED|0:0|worker-0
102|soperator-gpu-checks|COMPLETED|0:0|worker-1
102.batch|batch|COMPLETED|0:0|worker-1
`)
	expected := []framework.WorkerInfo{
		{Name: "worker-0"},
		{Name: "worker-1"},
	}
	require.NoError(t, assertGPUActiveCheckSlurmRecords(records, []string{"101", "102"}, expected))

	records["102"] = activeCheckSlurmJobRecord{ID: "102", State: "FAILED", ExitCode: "1:0", NodeList: "worker-1"}
	require.Error(t, assertGPUActiveCheckSlurmRecords(records, []string{"101", "102"}, expected))
}

func TestAssertGPUActiveCheckSlurmRecordsTargetExpectedWorkers(t *testing.T) {
	records := map[string]activeCheckSlurmJobRecord{
		"101": {ID: "101", State: "RUNNING", NodeList: "worker-0"},
		"102": {ID: "102", State: "RUNNING", NodeList: "worker-1"},
	}
	expected := []framework.WorkerInfo{
		{Name: "worker-0"},
		{Name: "worker-1"},
	}
	require.NoError(t, assertGPUActiveCheckSlurmRecordsTargetExpectedWorkers(records, []string{"101", "102"}, expected))

	records["102"] = activeCheckSlurmJobRecord{ID: "102", State: "RUNNING", NodeList: "worker-2"}
	require.Error(t, assertGPUActiveCheckSlurmRecordsTargetExpectedWorkers(records, []string{"101", "102"}, expected))
}

func TestActiveCheckSlurmJobRecordsTerminal(t *testing.T) {
	records := map[string]activeCheckSlurmJobRecord{
		"101": {ID: "101", State: "COMPLETED"},
		"102": {ID: "102", State: "RUNNING"},
	}
	assert.False(t, activeCheckSlurmJobRecordsTerminal(records, []string{"101", "102"}))

	records["102"] = activeCheckSlurmJobRecord{ID: "102", State: "FAILED"}
	assert.True(t, activeCheckSlurmJobRecordsTerminal(records, []string{"101", "102"}))
}

func TestAssertActiveGPUCheckOutputPassing(t *testing.T) {
	require.NoError(t, assertActiveGPUCheckOutputPassing("Health checker status: PASS\n"))
	require.Error(t, assertActiveGPUCheckOutputPassing("Health checker status: FAIL\n"))
	require.Error(t, assertActiveGPUCheckOutputPassing("no health status here\n"))
}

func TestParseGPUCount(t *testing.T) {
	count, err := parseGPUCount("worker-0", "8\n")
	require.NoError(t, err)
	assert.Equal(t, 8, count)

	_, err = parseGPUCount("worker-0", "0\n")
	require.Error(t, err)
}

func TestAllocMemPressureBytes(t *testing.T) {
	const gib = uint64(1024 * 1024 * 1024)

	pressure, err := allocMemPressureBytes(100*gib, 90*gib)
	require.NoError(t, err)
	assert.Equal(t, 12*gib, pressure)

	pressure, err = allocMemPressureBytes(80*gib, 90*gib)
	require.NoError(t, err)
	assert.Equal(t, allocMemPressureMinimum, pressure)

	_, err = allocMemPressureBytes(100*gib, 10*gib)
	require.Error(t, err)
}

func TestPodEphemeralLimitBytes(t *testing.T) {
	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceEphemeralStorage: resource.MustParse("2Gi"),
						},
					},
				},
			},
		},
	}
	assert.Equal(t, uint64(3*1024*1024*1024), podEphemeralLimitBytes(pod))
}

func TestParseKubectlDebugPodName(t *testing.T) {
	output := "Creating debugging pod node-debugger-worker-0-abcd with container debugger on node worker-0.\n"
	assert.Equal(t, "node-debugger-worker-0-abcd", parseKubectlDebugPodName(output))
	assert.Empty(t, parseKubectlDebugPodName("debug pod not found"))
}

func TestPeriodicHealthCheckWaitFollowsHealthCheckInterval(t *testing.T) {
	for _, tt := range []struct {
		name     string
		interval string
		want     time.Duration
		wantErr  string
	}{
		{name: "default interval", interval: "120 sec", want: 4 * time.Minute},
		{name: "short interval floors at the minimum", interval: "10 sec", want: 2 * time.Minute},
		{name: "at the limit", interval: "300 sec", want: 10 * time.Minute},
		{name: "too long", interval: "301 sec", wantErr: "too long"},
		{name: "disabled", interval: "0 sec", wantErr: "HealthCheckProgram is disabled"},
		{name: "unexpected format", interval: "2 min", wantErr: "parse HealthCheckInterval"},
		{name: "not a number", interval: "abc sec", wantErr: "parse HealthCheckInterval"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			timeout, err := periodicHealthCheckWait(map[string]string{"HealthCheckInterval": tt.interval})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, timeout)
		})
	}
}

func TestPeriodicHealthCheckWaitRequiresHealthCheckInterval(t *testing.T) {
	_, err := periodicHealthCheckWait(map[string]string{"SlurmdTimeout": "180 sec"})
	require.ErrorContains(t, err, "HealthCheckInterval is missing")
}

func TestParseUnixSeconds(t *testing.T) {
	parsed, err := parseUnixSeconds(" 1767225600\n")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), parsed)

	_, err = parseUnixSeconds("Thu Jan  1 00:00:00 UTC 2026")
	require.Error(t, err)
}

func TestWaitForFinishedCheckLog(t *testing.T) {
	read := func(outputs ...string) *periodicGPURuntime {
		reads := 0
		return &periodicGPURuntime{workers: map[string]framework.CommandScope{
			"worker-0": framework.NewCommandScope(func(context.Context, string) (string, error) {
				output := outputs[min(reads, len(outputs)-1)]
				reads++
				return output, nil
			}),
		}}
	}
	worker := framework.WorkerInfo{Name: "worker-0"}

	log, err := waitForFinishedCheckLog(t.Context(), read("", "Loaded libcuda.so.1\n", "Loaded libcuda.so.1\nAll 14 NVIDIA libraries are loadable\n"), worker, "/log", nvidiaLibrariesLogFinishedPattern, time.Minute)
	require.NoError(t, err)
	assert.Contains(t, log, "All 14 NVIDIA libraries are loadable")

	_, err = waitForFinishedCheckLog(t.Context(), read(""), worker, "/log", nvidiaLibrariesLogFinishedPattern, time.Minute)
	require.ErrorContains(t, err, "/log on worker-0 stayed empty or missing")

	_, err = waitForFinishedCheckLog(t.Context(), read("partial output"), worker, "/log", nvidiaLibrariesLogFinishedPattern, time.Minute)
	require.ErrorContains(t, err, "has no finished run")
	require.ErrorContains(t, err, "partial output")
}

func TestHealthCheckInterval(t *testing.T) {
	interval, err := healthCheckInterval(map[string]string{"HealthCheckInterval": "120 sec"})
	require.NoError(t, err)
	assert.Equal(t, 2*time.Minute, interval)

	_, err = healthCheckInterval(map[string]string{"HealthCheckInterval": "0 sec"})
	require.ErrorContains(t, err, "HealthCheckProgram is disabled")
	_, err = healthCheckInterval(map[string]string{})
	require.ErrorContains(t, err, "HealthCheckInterval is missing")
}

func TestHealthCheckReportRawOutputFiles(t *testing.T) {
	report := parsedPeriodicGPUReport(t, periodicGPUReportOptions{worker: "worker-0", runID: "run-0", status: "PASS", timestamp: periodicTestNotBefore})
	files, err := healthCheckReportRawOutputFiles(report)
	require.NoError(t, err)
	assert.Equal(t, []string{"nvidia_smi.run-0.out", "nvidia_smi_topo.run-0.out"}, files)

	report.Tests[1].State.Stdout = "../etc/passwd"
	_, err = healthCheckReportRawOutputFiles(report)
	require.ErrorContains(t, err, "outside the output directory")

	report.Tests = report.Tests[:1]
	_, err = healthCheckReportRawOutputFiles(report)
	require.ErrorContains(t, err, "references no raw command outputs")
}

func TestMissingFilesCommand(t *testing.T) {
	assert.Equal(t,
		`cd '/opt/out' && for f in 'a.out' 'b c.out'; do [ -f "$f" ] || printf '%s\n' "$f"; done`,
		missingFilesCommand("/opt/out", []string{"a.out", "b c.out"}),
	)
}
