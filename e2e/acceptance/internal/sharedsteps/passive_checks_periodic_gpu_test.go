package sharedsteps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

var periodicTestNotBefore = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

const periodicTestWorkerFacts = `kernel_version=6.11.0-1016-nvidia
gpu_driver_version=580.173.02
gpu_bios_version=97.00.D9.00.35
cuda_version=cuda_13.0.r13.0/compiler.36424714_0
dcgmi_version=4.7.0
lib_nccl_version=2.31.2-1+cuda13.0
mlnx_version=25.07-0.9.7
health_checker_version=1.1.38-ec83ad6d
`

func periodicTestFacts() map[string]string { return parseKeyValues(periodicTestWorkerFacts) }

type periodicGPURuntime struct {
	passiveCheckRuntime
	controller framework.CommandScope
	workers    map[string]framework.CommandScope
}

func (r *periodicGPURuntime) Controller() framework.CommandScope { return r.controller }
func (r *periodicGPURuntime) Logf(string, ...any)                {}
func (r *periodicGPURuntime) Worker(worker framework.WorkerInfo) framework.CommandScope {
	return r.workers[worker.Name]
}

type periodicGPUReportOptions struct {
	worker    string
	runID     string
	status    string
	timestamp time.Time
}

func periodicGPUReport(t *testing.T, opts periodicGPUReportOptions) string {
	t.Helper()
	report := map[string]any{
		"meta": map[string]any{
			"run_id":             opts.runID,
			"timestamp":          opts.timestamp.Unix(),
			"cuda_version":       "cuda_13.0.r13.0/compiler.36424714_0",
			"environment":        "SLURM_VERSION=26.05.4 SLURMD_NODENAME=" + opts.worker + " SLURM_CLUSTER_WITH_GPU=true",
			"gpu_driver_version": "580.173.02",
			"dcgmi_version":      "4.7.0",
			"lib_nccl_version":   "2.31.2-1+cuda13.0",
			"mlnx_version":       "25.07-0.9.7",
			"kernel_version":     "6.11.0-1016-nvidia",
			"gpu_bios_version":   "97.00.D9.00.35",
		},
		"tests": []map[string]any{
			{"name": "module", "suppressed_stdout": false, "state": map[string]any{"stdout": ""}},
			{"name": "nvidia_smi", "suppressed_stdout": false, "state": map[string]any{"stdout": "nvidia_smi." + opts.runID + ".out", "stderr": "nvidia_smi." + opts.runID + ".out"}},
			{"name": "nvidia_smi_topo", "suppressed_stdout": false, "state": map[string]any{"stdout": "nvidia_smi_topo." + opts.runID + ".out"}},
			{"name": "dmesg", "suppressed_stdout": true, "state": map[string]any{"stdout": "dmesg." + opts.runID + ".out"}},
		},
		"statuses": map[string]any{"PASS": []string{"module", "nvidia_smi"}, "EMPTY": []string{"nvidia_smi.sm_clock"}},
		"status":   opts.status,
	}
	if opts.status == "FAIL" {
		report["statuses"] = map[string]any{"PASS": []string{"module"}, "FAIL": []string{"nvidia_smi.gpu_count"}}
	}
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	return "All checks passed\nHealth checker exit code: 0\nHealth checker stdout:\n" + string(encoded) + "\nHealth checker stderr:\n"
}

func parsedPeriodicGPUReport(t *testing.T, opts periodicGPUReportOptions) healthCheckReport {
	t.Helper()
	reports, err := parseHealthCheckReports(periodicGPUReport(t, opts))
	require.NoError(t, err)
	require.Len(t, reports, 1)
	return reports[0]
}

func TestPeriodicGPUHealthCheckWaitsForReportsNewerThanSelection(t *testing.T) {
	// Completed in the same second as the selection: only the unchanged run ID tells it is not new.
	baselineReport := periodicGPUReport(t, periodicGPUReportOptions{worker: "worker-0", runID: "baseline-0", status: "PASS", timestamp: periodicTestNotBefore})
	freshReports := map[string]string{
		"worker-0": periodicGPUReport(t, periodicGPUReportOptions{worker: "worker-0", runID: "run-0", status: "PASS", timestamp: periodicTestNotBefore.Add(time.Second)}),
		"worker-1": periodicGPUReport(t, periodicGPUReportOptions{worker: "worker-1", runID: "run-1", status: "PASS", timestamp: periodicTestNotBefore}),
	}
	reads := map[string]int{}
	var rawCommands []string
	workerScope := func(name string) framework.CommandScope {
		return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
			switch {
			case strings.Contains(command, "/opt/soperator-outputs/local/slurm_scripts/"+name+".gpu_health_check.hc_program.out"):
				reads[name]++
				if name == "worker-0" && reads[name] == 1 {
					return baselineReport, nil
				}
				return freshReports[name], nil
			case command == framework.ShellJoin("bash", "-lc", gpuWorkerFactsScript):
				return periodicTestWorkerFacts, nil
			case strings.HasPrefix(command, "cd '/opt/soperator-outputs/local/health_checker_cmd_stdout' && "):
				rawCommands = append(rawCommands, command)
				return "", nil
			default:
				t.Fatalf("unexpected command on %s: %s", name, command)
				return "", nil
			}
		})
	}
	runtime := &periodicGPURuntime{
		controller: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
			require.Equal(t, "scontrol show config", command)
			return "HealthCheckInterval     = 120 sec\nHealthCheckNodeState    = CYCLE,ANY\nHealthCheckProgram      = /opt/slurm_scripts/hc_program.sh\n", nil
		}),
		workers: map[string]framework.CommandScope{"worker-0": workerScope("worker-0"), "worker-1": workerScope("worker-1")},
	}
	steps := PassiveChecks{
		info:    &framework.ClusterInfo{TargetSoperatorVersion: "5.0.0"},
		runtime: runtime,
		slurm:   framework.NewSlurmClient(runtime),
		periodicGPUChecks: []periodicGPUHealthCheck{
			{worker: framework.WorkerInfo{Name: "worker-0"}, notBefore: periodicTestNotBefore, baselineRunID: "baseline-0"},
			{worker: framework.WorkerInfo{Name: "worker-1"}, notBefore: periodicTestNotBefore},
		},
	}

	require.NoError(t, steps.aNewPeriodicGPUHealthCheckCompletesOnEverySelectedGPUWorker(t.Context()))
	assert.Equal(t, map[string]int{"worker-0": 2, "worker-1": 1}, reads)
	assert.Equal(t, "run-0", healthCheckRunID(steps.periodicGPUChecks[0].report))
	assert.Equal(t, "run-1", healthCheckRunID(steps.periodicGPUChecks[1].report))

	require.NoError(t, steps.everyNewPeriodicGPUHealthCheckReportIsPassing(t.Context()))
	require.NoError(t, steps.everyNewPeriodicGPUHealthCheckReportHasMetadataMatchingItsWorker(t.Context()))
	require.NoError(t, steps.everyNewPeriodicGPUHealthCheckReportHasItsRawCommandOutputs(t.Context()))
	require.Len(t, rawCommands, 2)
	assert.Contains(t, rawCommands[0], "'nvidia_smi.run-0.out' 'nvidia_smi_topo.run-0.out';")
	assert.NotContains(t, rawCommands[0], "dmesg")
}

func TestPeriodicGPUHealthCheckTimeoutNamesPlatform(t *testing.T) {
	staleReport := periodicGPUReport(t, periodicGPUReportOptions{worker: "worker-0", runID: "old", status: "PASS", timestamp: periodicTestNotBefore.Add(-time.Minute)})
	runtime := &periodicGPURuntime{workers: map[string]framework.CommandScope{
		"worker-0": framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
			if strings.Contains(command, "check_runner.hc_program.out") {
				return `[2026-01-01 00:00:00.000 UTC] INFO: Environment CHECKS_PLATFORM_TAG="8xL40S"` + "\n", nil
			}
			return staleReport, nil
		}),
	}}
	steps := PassiveChecks{info: &framework.ClusterInfo{TargetSoperatorVersion: "5.0.0"}, runtime: runtime}
	_, err := steps.waitForPeriodicGPUHealthCheckReport(t.Context(), periodicGPUHealthCheck{worker: framework.WorkerInfo{Name: "worker-0"}, notBefore: periodicTestNotBefore}, time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CHECKS_PLATFORM_TAG=8xL40S")
	assert.Contains(t, err.Error(), `"run_id":"old"`)
}

func TestPeriodicGPUHealthCheckReportsNonPassingChecks(t *testing.T) {
	steps := PassiveChecks{periodicGPUChecks: []periodicGPUHealthCheck{
		{worker: framework.WorkerInfo{Name: "worker-0"}, report: parsedPeriodicGPUReport(t, periodicGPUReportOptions{worker: "worker-0", runID: "run-0", status: "PASS", timestamp: periodicTestNotBefore})},
		{worker: framework.WorkerInfo{Name: "worker-1"}, report: parsedPeriodicGPUReport(t, periodicGPUReportOptions{worker: "worker-1", runID: "run-1", status: "FAIL", timestamp: periodicTestNotBefore})},
		{worker: framework.WorkerInfo{Name: "worker-2"}, report: parsedPeriodicGPUReport(t, periodicGPUReportOptions{worker: "worker-2", runID: "run-2", status: "ERROR", timestamp: periodicTestNotBefore})},
	}}
	err := steps.everyNewPeriodicGPUHealthCheckReportIsPassing(t.Context())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "worker-0")
	assert.Contains(t, err.Error(), `worker-1: expected health-check status PASS, got "FAIL"; non-passing checks: FAIL: nvidia_smi.gpu_count`)
	assert.Contains(t, err.Error(), `worker-2: expected health-check status PASS, got "ERROR"`)

	steps.periodicGPUChecks = []periodicGPUHealthCheck{{worker: framework.WorkerInfo{Name: "worker-0"}}}
	require.ErrorContains(t, steps.everyNewPeriodicGPUHealthCheckReportIsPassing(t.Context()), "report is not captured")
}

func TestAssertGPUHealthCheckMeta(t *testing.T) {
	report := parsedPeriodicGPUReport(t, periodicGPUReportOptions{worker: "worker-0", runID: "run-0", status: "PASS", timestamp: periodicTestNotBefore})
	require.NoError(t, assertGPUHealthCheckMeta(report, "worker-0", periodicTestFacts()))
	require.ErrorContains(t, assertGPUHealthCheckMeta(report, "worker-10", periodicTestFacts()), "does not contain SLURMD_NODENAME=worker-10")

	for _, source := range gpuHealthCheckMetaSources {
		t.Run(source.field, func(t *testing.T) {
			facts := periodicTestFacts()
			facts[source.field] = "other"
			require.ErrorContains(t, assertGPUHealthCheckMeta(report, "worker-0", facts),
				fmt.Sprintf("meta.%s is %q, %s on the worker reports \"other\"", source.field, report.Meta[source.field], source.source))

			delete(facts, source.field)
			require.ErrorContains(t, assertGPUHealthCheckMeta(report, "worker-0", facts), "for meta."+source.field+" on the worker: no value")
		})
	}

	for name, mutate := range map[string]func(map[string]any){
		"missing timestamp": func(meta map[string]any) { delete(meta, "timestamp") },
		"missing run ID":    func(meta map[string]any) { delete(meta, "run_id") },
	} {
		t.Run(name, func(t *testing.T) {
			report := parsedPeriodicGPUReport(t, periodicGPUReportOptions{worker: "worker-0", runID: "run-0", status: "PASS", timestamp: periodicTestNotBefore})
			mutate(report.Meta)
			require.Error(t, assertGPUHealthCheckMeta(report, "worker-0", periodicTestFacts()))
		})
	}
}

func TestIsFreshPeriodicGPUHealthCheckReport(t *testing.T) {
	check := periodicGPUHealthCheck{notBefore: periodicTestNotBefore, baselineRunID: "baseline"}
	for _, tt := range []struct {
		name      string
		runID     string
		timestamp time.Time
		want      bool
	}{
		{name: "new run in the selection second", runID: "new", timestamp: periodicTestNotBefore, want: true},
		{name: "new run after selection", runID: "new", timestamp: periodicTestNotBefore.Add(time.Minute), want: true},
		{name: "baseline run in the selection second", runID: "baseline", timestamp: periodicTestNotBefore},
		{name: "older run", runID: "older", timestamp: periodicTestNotBefore.Add(-time.Second)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			report := parsedPeriodicGPUReport(t, periodicGPUReportOptions{worker: "worker-0", runID: tt.runID, status: "PASS", timestamp: tt.timestamp})
			fresh, err := isFreshPeriodicGPUHealthCheckReport(report, check)
			require.NoError(t, err)
			assert.Equal(t, tt.want, fresh)
		})
	}
}

func TestPeriodicGPUHealthCheckBaselineRunID(t *testing.T) {
	complete := periodicGPUReport(t, periodicGPUReportOptions{worker: "worker-0", runID: "baseline-0", status: "PASS", timestamp: periodicTestNotBefore})
	for _, tt := range []struct {
		name    string
		outputs []string
		want    string
	}{
		{name: "no report yet", outputs: []string{""}, want: ""},
		{name: "report being rewritten", outputs: []string{"All checks passed\n{\"meta\":", complete}, want: "baseline-0"},
		{name: "run without a report", outputs: []string{"Unexpected result\n"}, want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reads := 0
			runtime := &periodicGPURuntime{workers: map[string]framework.CommandScope{
				"worker-0": framework.NewCommandScope(func(context.Context, string) (string, error) {
					output := tt.outputs[min(reads, len(tt.outputs)-1)]
					reads++
					return output, nil
				}),
			}}
			steps := PassiveChecks{info: &framework.ClusterInfo{TargetSoperatorVersion: "5.0.0"}, runtime: runtime}
			assert.Equal(t, tt.want, steps.periodicGPUHealthCheckBaselineRunID(t.Context(), framework.WorkerInfo{Name: "worker-0"}))
		})
	}
}

func TestHealthCheckReportTimestamp(t *testing.T) {
	report := parsedPeriodicGPUReport(t, periodicGPUReportOptions{worker: "worker-0", runID: "run-0", status: "PASS", timestamp: periodicTestNotBefore})
	reportedAt, err := healthCheckReportTimestamp(report)
	require.NoError(t, err)
	assert.Equal(t, periodicTestNotBefore, reportedAt)

	report.Meta["timestamp"] = "1767225600"
	_, err = healthCheckReportTimestamp(report)
	require.Error(t, err)
}
