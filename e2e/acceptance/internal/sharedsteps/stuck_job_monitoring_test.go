package sharedsteps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func stuckJobMonitoringTestSeries(state, drain, reason string, timestamps ...int64) stuckJobMetricSeries {
	series := stuckJobMetricSeries{Metric: map[string]string{
		"node_name": "worker-0", "instance_id": "instance-0", "state_base": state,
		"state_is_drain": drain, "reason": reason,
	}}
	for _, timestamp := range timestamps {
		series.Values = append(series.Values, [2]json.RawMessage{json.RawMessage(strconv.FormatInt(timestamp, 10)), json.RawMessage(`"1"`)})
	}
	return series
}

func TestStuckJobMonitoringRequiresFreshOrderedSamplesForTheSelectedInstance(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func([]stuckJobMetricSeries)
		err    string
	}{
		{name: "complete timeline"},
		{name: "another drain reason", change: func(s []stuckJobMetricSeries) { s[0].Metric["reason"] = "customer drain" }, err: "no DRAIN sample"},
		{name: "missing DOWN", change: func(s []stuckJobMetricSeries) { s[1].Metric["state_base"] = "IDLE" }, err: "no DOWN sample"},
		{name: "still drained", change: func(s []stuckJobMetricSeries) { s[2].Metric["state_is_drain"] = "true" }, err: "no IDLE sample"},
		{name: "old drain", change: func(s []stuckJobMetricSeries) { s[0].Values[0][0] = json.RawMessage(`90`) }, err: "no DRAIN sample"},
		{name: "wrong order", change: func(s []stuckJobMetricSeries) { s[1].Values[0][0] = json.RawMessage(`105`) }, err: "no DOWN sample"},
		{name: "another worker", change: func(s []stuckJobMetricSeries) { s[1].Metric["node_name"] = "worker-1" }, err: "no DOWN sample"},
		{name: "another instance", change: func(s []stuckJobMetricSeries) { s[1].Metric["instance_id"] = "previous-instance" }, err: "no DOWN sample"},
		{name: "zero-valued metric", change: func(s []stuckJobMetricSeries) { s[1].Values[0][1] = json.RawMessage(`"0"`) }, err: "no DOWN sample"},
		{name: "invalid timestamp", change: func(s []stuckJobMetricSeries) { s[0].Values[0][0] = json.RawMessage(`"bad"`) }, err: "parse monitoring sample timestamp"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var series = []stuckJobMetricSeries{
				stuckJobMonitoringTestSeries("IDLE", "true", "Kill task failed (JobId=123)", 110),
				stuckJobMonitoringTestSeries("DOWN", "true", "node reboot process", 120),
				stuckJobMonitoringTestSeries("IDLE", "false", "", 50, 130),
			}
			if tt.change != nil {
				tt.change(series)
			}
			worker := framework.WorkerInfo{Name: "worker-0", SlurmNode: framework.SlurmNodeInfo{InstanceID: "instance-0"}}
			err := checkStuckJobMetricPhases(series, worker, time.Unix(100, 0), []string{"DRAIN", "DOWN", "IDLE"})
			if tt.err == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.err)
			}
		})
	}
}

func TestStuckJobMonitoringPreflight(t *testing.T) {
	for _, tt := range []struct {
		name, services, releases, err string
		queryFailure, missingSamples  bool
		skip                          bool
	}{
		{name: "available"},
		{name: "disabled", services: `{"items":[]}`, skip: true},
		{name: "configured service missing", services: `{"items":[]}`, releases: `{"items":[{"metadata":{"name":"test-vm-stack"}}]}`, err: "monitoring is configured"},
		{name: "access denied", queryFailure: true, err: "forbidden"},
		{name: "exporter data missing", missingSamples: true, err: "no IDLE sample"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			steps, runtime := stuckJobTestSteps(t, "IDLE", "before")
			runtime.kubectl = rolloutCleanupArgsScope{ArgsScope: framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
				switch args[1] {
				case "services":
					if tt.services != "" {
						return tt.services, nil
					}
					return `{"items":[{"metadata":{"name":"vmsingle-test"},"spec":{"ports":[{"port":18428}]}}]}`, nil
				case "helmreleases":
					if tt.releases != "" {
						return tt.releases, nil
					}
					return `{"items":[]}`, nil
				default:
					require.Equal(t, "--raw", args[1])
					if tt.queryFailure {
						return "", fmt.Errorf("forbidden")
					}
					request, err := url.Parse(args[2])
					require.NoError(t, err)
					assert.Equal(t, "/api/v1/namespaces/monitoring-system/services/vmsingle-test:18428/proxy/api/v1/query", request.Path)
					assert.Contains(t, request.Query().Get("query"), `slurm_node_info{node_name="worker-0",instance_id="instance-0"}[`)
					response := stuckJobMetricResponse{Status: "success"}
					response.Data.ResultType = "matrix"
					if !tt.missingSamples {
						response.Data.Result = []stuckJobMetricSeries{stuckJobMonitoringTestSeries("IDLE", "false", "", time.Now().Unix())}
					}
					data, err := json.Marshal(response)
					return string(data), err
				}
			})}
			err := steps.monitoringRecordsWorker(t.Context())
			switch {
			case tt.skip:
				require.ErrorIs(t, err, godog.ErrSkip)
			case tt.err != "":
				require.ErrorContains(t, err, tt.err)
			default:
				require.NoError(t, err)
			}
		})
	}
}
