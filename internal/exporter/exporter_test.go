package exporter

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"nebius.ai/slurm-operator/internal/slurmapi"
	"nebius.ai/slurm-operator/internal/slurmapi/fake"
)

func TestExporterCollectionLoopSkipsRunningSubCollector(t *testing.T) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))

	mockClient := &fake.MockClient{}
	var jobsCalls atomic.Int32
	jobsStarted := make(chan struct{})
	jobsDone := make(chan struct{})
	releaseJobs := make(chan struct{})
	var closeStarted, closeDone sync.Once

	mockClient.EXPECT().ListNodes(mock.Anything).Return([]slurmapi.Node{}, nil).Maybe()
	mockClient.EXPECT().GetDiag(mock.Anything).Return(&api.V0041OpenapiDiagResp{}, nil).Maybe()
	mockClient.EXPECT().ListJobsWithParams(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, _ slurmapi.ListJobsParams) ([]slurmapi.Job, error) {
			jobsCalls.Add(1)
			closeStarted.Do(func() { close(jobsStarted) })
			defer closeDone.Do(func() { close(jobsDone) })

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-releaseJobs:
				return nil, nil
			}
		},
	)

	exporter := NewClusterExporter(mockClient, Params{CollectionInterval: 5 * time.Millisecond})
	registry := prometheus.NewRegistry()
	require.NoError(t, exporter.monitoringMetrics.Register(registry))

	ctx, cancel := context.WithCancel(context.Background())
	loopDone := make(chan struct{})
	go func() {
		exporter.collectionLoop(ctx)
		close(loopDone)
	}()

	select {
	case <-jobsStarted:
	case <-time.After(time.Second):
		t.Fatal("jobs collector did not start")
	}

	require.Eventually(t, func() bool {
		families, err := registry.Gather()
		require.NoError(t, err)
		return collectorCounterValue(families, "slurm_exporter_collector_skipped_total", "jobs") > 0
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(1), jobsCalls.Load())

	cancel()
	close(releaseJobs)

	select {
	case <-jobsDone:
	case <-time.After(time.Second):
		t.Fatal("jobs collector did not stop")
	}
	select {
	case <-loopDone:
	case <-time.After(time.Second):
		t.Fatal("collection loop did not stop")
	}
}
