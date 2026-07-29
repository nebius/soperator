package steps

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

func TestLoggedCheckOutputPathsNormalizesJailPrefix(t *testing.T) {
	output := `
[2026-01-01 00:00:00.000 UTC] INFO: Running check one (true), logging to /mnt/jail/opt/soperator-outputs/slurm_scripts/worker-0.one.prolog.out
[2026-01-01 00:00:01.000 UTC] INFO: Running check two (true), logging to /opt/soperator-outputs/slurm_scripts/worker-0.two.prolog.out
[2026-01-01 00:00:02.000 UTC] INFO: Running check one (true), logging to /mnt/jail/opt/soperator-outputs/slurm_scripts/worker-0.one.prolog.out
`
	assert.Equal(t, []string{
		"/opt/soperator-outputs/slurm_scripts/worker-0.one.prolog.out",
		"/opt/soperator-outputs/slurm_scripts/worker-0.two.prolog.out",
	}, loggedCheckOutputPaths(output))
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
	name := uniqueK8sJobName("logs-cleaner")
	assert.LessOrEqual(t, len(name), 63)
	assert.Contains(t, name, "acceptance-logs-cleaner-")
}

func TestAssertGPUActiveCheckSlurmRecords(t *testing.T) {
	records := parseActiveCheckSlurmJobRecords(`
101|soperator-gpu-checks|COMPLETED|0:0|worker-0
101.batch|batch|COMPLETED|0:0|worker-0
102|soperator-gpu-checks|COMPLETED|0:0|worker-1
102.batch|batch|COMPLETED|0:0|worker-1
`)
	expected := []framework.WorkerRef{
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
	expected := []framework.WorkerRef{
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
