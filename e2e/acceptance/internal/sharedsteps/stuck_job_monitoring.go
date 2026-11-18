package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const stuckJobMonitoringTimeout = 2 * time.Minute

type stuckJobMonitoring struct {
	path  string
	since time.Time
}

type stuckJobMetricSeries struct {
	Metric map[string]string    `json:"metric"`
	Values [][2]json.RawMessage `json:"values"`
}

type stuckJobMetricResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Data   struct {
		ResultType string                 `json:"resultType"`
		Result     []stuckJobMetricSeries `json:"result"`
	} `json:"data"`
}

func (s *StuckJobRecovery) monitoringRecordsWorker(ctx context.Context) error {
	var services corev1.ServiceList
	if err := s.kubectl.GetJSON(ctx, &services, "get", "services", "-n", observabilityNamespace,
		"-l", "app.kubernetes.io/name=vmsingle", "-o", "json"); err != nil {
		return err
	}
	if len(services.Items) == 0 {
		var releases helmReleaseList
		if err := s.kubectl.GetJSON(ctx, &releases, "get", "helmreleases", "-A", "-o", "json"); err != nil {
			return err
		}
		for _, release := range releases.Items {
			if strings.HasSuffix(release.Metadata.Name, "-vm-stack") {
				return fmt.Errorf("monitoring is configured by HelmRelease %s/%s but its VictoriaMetrics service is missing", release.Metadata.Namespace, release.Metadata.Name)
			}
		}
		s.runtime.Logf("Skipping monitoring recovery scenario: VictoriaMetrics is not configured")
		return godog.ErrSkip
	}
	if len(services.Items) != 1 || len(services.Items[0].Spec.Ports) != 1 {
		return fmt.Errorf("discover VictoriaMetrics: expected one service with one HTTP port, got %d services", len(services.Items))
	}
	service := services.Items[0]
	s.monitoring = &stuckJobMonitoring{
		path:  fmt.Sprintf("/api/v1/namespaces/%s/services/%s:%d/proxy/api/v1/query", observabilityNamespace, service.Name, service.Spec.Ports[0].Port),
		since: time.Now().Add(-stuckJobMonitoringTimeout),
	}
	return s.runtime.WaitFor(ctx, "monitoring records idle worker "+s.worker.Name, stuckJobMonitoringTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			response, err := s.queryMonitoring(waitCtx)
			if err != nil {
				return false, err
			}
			err = checkStuckJobMetricPhases(response.Data.Result, s.worker, s.monitoring.since, []string{"IDLE"})
			return err == nil, err
		})
}

func (s *StuckJobRecovery) queryMonitoring(ctx context.Context) (stuckJobMetricResponse, error) {
	now := time.Now()
	seconds := max(1, int(now.Sub(s.monitoring.since).Seconds())+1)
	parameters := make(url.Values)
	// An instant query of a range vector returns stored samples with their original
	// timestamps. query_range would evaluate expressions at new timestamps instead.
	parameters.Set("query", fmt.Sprintf("slurm_node_info{node_name=%q,instance_id=%q}[%ds]", s.worker.Name, s.worker.SlurmNode.InstanceID, seconds))
	parameters.Set("time", strconv.FormatFloat(float64(now.UnixMilli())/1000, 'f', 3, 64))
	var response stuckJobMetricResponse
	if err := s.kubectl.GetJSON(ctx, &response, "get", "--raw", s.monitoring.path+"?"+parameters.Encode()); err != nil {
		return response, err
	}
	if response.Status != "success" || response.Data.ResultType != "matrix" {
		return response, fmt.Errorf("query worker monitoring: status=%s resultType=%s error=%s", response.Status, response.Data.ResultType, response.Error)
	}
	return response, nil
}

func (s *StuckJobRecovery) monitoringRecordsRecovery(ctx context.Context) error {
	var response stuckJobMetricResponse
	err := s.runtime.WaitFor(ctx, "monitoring records recovery of worker "+s.worker.Name, stuckJobMonitoringTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			var err error
			response, err = s.queryMonitoring(waitCtx)
			if err != nil {
				return false, err
			}
			err = checkStuckJobMetricPhases(response.Data.Result, s.worker, s.monitoring.since, []string{"DRAIN", "DOWN", "IDLE"})
			return err == nil, err
		})
	if artifacts, ok := framework.ScenarioArtifacts(ctx); ok {
		data, marshalErr := json.MarshalIndent(response, "", "  ")
		if marshalErr != nil {
			return errors.Join(err, marshalErr)
		}
		err = errors.Join(err, artifacts.WriteRunnerFile("stuck-job-metrics.json", string(data)))
	}
	if err == nil {
		s.runtime.Logf("Monitoring recorded Kill task failed drain, DOWN and recovered IDLE for worker %s", s.worker.Name)
	}
	return err
}

func checkStuckJobMetricPhases(series []stuckJobMetricSeries, worker framework.WorkerInfo, since time.Time, phases []string) error {
	after := float64(since.UnixMilli()) / 1000
	for _, phase := range phases {
		var first float64
		for _, item := range series {
			labels := item.Metric
			if labels["node_name"] != worker.Name || labels["instance_id"] != worker.SlurmNode.InstanceID {
				continue
			}
			matches := phase == "DRAIN" && labels["state_is_drain"] == "true" && strings.Contains(labels["reason"], stuckJobDrainReason) ||
				phase == "DOWN" && labels["state_base"] == "DOWN" ||
				phase == "IDLE" && labels["state_base"] == "IDLE" && labels["state_is_drain"] == "false"
			if !matches {
				continue
			}
			for _, pair := range item.Values {
				var timestamp float64
				var value string
				if err := json.Unmarshal(pair[0], &timestamp); err != nil {
					return fmt.Errorf("parse monitoring sample timestamp: %w", err)
				}
				if err := json.Unmarshal(pair[1], &value); err != nil {
					return fmt.Errorf("parse monitoring sample value: %w", err)
				}
				if value == "1" && timestamp > after && (first == 0 || timestamp < first) {
					first = timestamp
				}
			}
		}
		if first == 0 {
			return fmt.Errorf("monitoring has no %s sample for worker %s after %.3f", phase, worker.Name, after)
		}
		after = first
	}
	return nil
}
