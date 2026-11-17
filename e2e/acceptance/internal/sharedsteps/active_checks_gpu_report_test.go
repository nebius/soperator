package sharedsteps

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const healthCheckerActiveListOutput = `all_reduce_with_ib: all_reduce_with_ib
	avgbusbw: assert average bus bandwidth is higher than 30.00 GB/s
	nvls: assert NVLS multicast groups were not created
dcgmi_diag_r2: Medium dcgmi diagnostic
	status: check status
deviceQuery: deviceQuery
ib_write_bw_gpu_0_1: ib_write_bw_gpu_0_1 GPU output
	ib_perf_bw: assert average bandwidth is not lower than 127.5 Gb/s
`

func activeGPUCheckReportJSON(t *testing.T, runID string, statusesByName map[string]string, indent bool) string {
	t.Helper()
	report := map[string]any{
		"meta": map[string]any{"run_id": runID},
		"tests": []map[string]any{
			{"name": "all_reduce_with_ib", "state": map[string]any{"stdout": "all_reduce_with_ib." + runID + ".out"}},
			{"name": "dcgmi_diag_r2", "state": map[string]any{"stdout": "dcgmi_diag_r2." + runID + ".out"}},
			{"name": "deviceQuery", "state": map[string]any{"stdout": ""}},
		},
		"statuses_by_name": statusesByName,
		"status":           "PASS",
	}
	var encoded []byte
	var err error
	if indent {
		encoded, err = json.MarshalIndent(report, "", "  ")
	} else {
		encoded, err = json.Marshal(report)
	}
	require.NoError(t, err)
	return string(encoded)
}

func activeGPUCheckJobOutput(platform, report string) string {
	return "Platform found: " + platform + "\nRunning GPU checks on worker-0...\nHealth checker output:\n" + report +
		"\nHealth checker status: PASS\nHealth-checker passed or returned non-FAIL status.\n"
}

func passingActiveStatuses() map[string]string {
	return map[string]string{
		"all_reduce_with_ib": "PASS", "all_reduce_with_ib.avgbusbw": "PASS",
		"dcgmi_diag_r2": "PASS", "deviceQuery": "PASS", "ib_write_bw_gpu_0_1": "PASS",
		"sm_clock": "EMPTY",
	}
}

func TestParseHealthCheckerTestList(t *testing.T) {
	assert.Equal(t,
		[]string{"all_reduce_with_ib", "dcgmi_diag_r2", "deviceQuery", "ib_write_bw_gpu_0_1"},
		parseHealthCheckerTestList(healthCheckerActiveListOutput+"deviceQuery: deviceQuery\n\nWARN something went wrong\n"),
	)
	assert.Empty(t, parseHealthCheckerTestList("\n"))
}

func TestParseActiveGPUCheckOutput(t *testing.T) {
	for name, indent := range map[string]bool{"json-partial line": false, "pretty-printed": true} {
		t.Run(name, func(t *testing.T) {
			parsed, err := parseActiveGPUCheckOutput(activeGPUCheckJobOutput("8xB200", activeGPUCheckReportJSON(t, "run-0", passingActiveStatuses(), indent)))
			require.NoError(t, err)
			assert.Equal(t, "8xB200", parsed.platform)
			assert.Equal(t, "run-0", healthCheckRunID(parsed.report))
			assert.Equal(t, "PASS", parsed.report.StatusesByName["dcgmi_diag_r2"])
		})
	}

	for name, output := range map[string]string{
		"no platform":        strings.Replace(activeGPUCheckJobOutput("8xB200", `{"status":"PASS"}`), "Platform found: 8xB200\n", "", 1),
		"unsupported output": "Unsupported platform\n",
		"invalid platform":   activeGPUCheckJobOutput("8xB200; rm -rf /", `{"status":"PASS"}`),
		"no report":          activeGPUCheckJobOutput("8xB200", "srun: error: worker-0: task 0: Exited with exit code 1"),
		"no output section":  "Platform found: 8xB200\nHealth checker status: PASS\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseActiveGPUCheckOutput(output)
			require.Error(t, err)
		})
	}
}

func TestAssertHealthCheckTestsPassed(t *testing.T) {
	expected := parseHealthCheckerTestList(healthCheckerActiveListOutput)
	report := healthCheckReport{Meta: map[string]any{"run_id": "run-0"}, StatusesByName: passingActiveStatuses()}
	require.NoError(t, assertHealthCheckTestsPassed(report, expected))

	report.StatusesByName["dcgmi_diag_r2"] = "EMPTY"
	report.StatusesByName["deviceQuery"] = "FAIL"
	delete(report.StatusesByName, "ib_write_bw_gpu_0_1")
	err := assertHealthCheckTestsPassed(report, expected)
	require.Error(t, err)
	assert.Equal(t, "run run-0 did not pass 3 of 4 expected tests: dcgmi_diag_r2: EMPTY, deviceQuery: FAIL, ib_write_bw_gpu_0_1: missing", err.Error())
}

func TestGPUActiveCheckReportsAreComparedWithTheirPlatformSuite(t *testing.T) {
	var listCommands, rawCommands []string
	workerScope := func(missingRaw string) framework.CommandScope {
		return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
			switch {
			case strings.Contains(command, "health-checker"):
				listCommands = append(listCommands, command)
				return healthCheckerActiveListOutput, nil
			case strings.HasPrefix(command, "cd '/opt/soperator-outputs/local/health_checker_cmd_stdout' && "):
				rawCommands = append(rawCommands, command)
				return missingRaw, nil
			default:
				t.Fatalf("unexpected worker command: %s", command)
				return "", nil
			}
		})
	}
	failing := passingActiveStatuses()
	failing["all_reduce_with_ib"] = "EMPTY"
	runtime := &periodicGPURuntime{workers: map[string]framework.CommandScope{
		"worker-0": workerScope(""),
		"worker-1": workerScope("dcgmi_diag_r2.run-1.out\n"),
	}}
	steps := ActiveChecks{
		info:    &framework.ClusterInfo{TargetSoperatorVersion: "5.0.0"},
		runtime: runtime,
		gpuCheckOutputs: map[string]activeGPUCheckOutput{
			"11": {record: activeCheckSlurmJobRecord{ID: "11", NodeList: "worker-0"}, output: activeGPUCheckJobOutput("8xB200", activeGPUCheckReportJSON(t, "run-0", passingActiveStatuses(), false))},
			"12": {record: activeCheckSlurmJobRecord{ID: "12", NodeList: "worker-1"}, output: activeGPUCheckJobOutput("8xB200", activeGPUCheckReportJSON(t, "run-1", failing, false))},
		},
	}

	err := steps.everyGPUActiveCheckReportPassedEveryActiveCheckTestForItsPlatform(t.Context())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "worker-0")
	assert.Contains(t, err.Error(), "worker-1 (job 12): run run-1 did not pass 1 of 4 expected tests: all_reduce_with_ib: EMPTY")
	require.Len(t, listCommands, 2)
	assert.Equal(t, healthCheckerListCommand("soperator_active_checks", "8xB200"), listCommands[0])

	err = steps.everyGPUActiveCheckReportHasItsRawCommandOutputs(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "worker-1 (job 12): raw health-check outputs of run run-1 are missing from /opt/soperator-outputs/local/health_checker_cmd_stdout: dcgmi_diag_r2.run-1.out")
	require.Len(t, rawCommands, 2)
	assert.Contains(t, rawCommands[0], "'all_reduce_with_ib.run-0.out' 'dcgmi_diag_r2.run-0.out';")

	steps.gpuCheckOutputs = nil
	require.ErrorContains(t, steps.everyGPUActiveCheckReportHasItsRawCommandOutputs(t.Context()), "outputs are not captured")
}

func TestHealthCheckerListCommand(t *testing.T) {
	assert.Equal(t, `bash -lc 'health-checker list -e soperator_active_checks -p 8xB200'`, healthCheckerListCommand("soperator_active_checks", "8xB200"))
}
