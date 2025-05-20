package exporter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"nebius.ai/slurm-operator/internal/slurmapi"
)

// ControllerName is the name of the SLURM metrics exporter component
var ControllerName = "soperator-exporter"

// Params contains configuration parameters for the SLURM metrics exporter
type Params struct {
	// SlurmAPIServer is the URL of the SLURM REST API server
	SlurmAPIServer string
	// ScrapeInterval is the interval between metric collection runs
	ScrapeInterval time.Duration
	// SlurmClusterID is the namespaced name of the SlurmCluster resource
	SlurmClusterID types.NamespacedName
}

// Exporter collects metrics from a SLURM cluster and exports them in Prometheus format
type Exporter struct {
	// params stores the configuration parameters
	params Params
	// slurmAPIClient is the client for the SLURM REST API
	slurmAPIClient slurmapi.Client
	// registry is the Prometheus registry for the metrics
	registry *prometheus.Registry
	// collector is the metrics collector
	collector *MetricsCollector
	// httpServer is the HTTP server for the metrics endpoint
	httpServer *http.Server
	// stopCh is used to signal the exporter to stop
	stopCh chan struct{}
}

// NewClusterExporter creates a new SLURM cluster exporter
func NewClusterExporter(
	slurmAPIClient slurmapi.Client,
	params Params,
) (*Exporter, error) {
	if slurmAPIClient == nil {
		return nil, errors.New("slurmAPIClient cannot be nil")
	}

	if params.ScrapeInterval <= 0 {
		return nil, errors.New("scrapeInterval must be positive")
	}

	registry := prometheus.NewRegistry()
	collector := NewMetricsCollector()

	return &Exporter{
		params:         params,
		slurmAPIClient: slurmAPIClient,
		registry:       registry,
		collector:      collector,
		stopCh:         make(chan struct{}),
	}, nil
}

// Start starts the SLURM metrics exporter
func (e *Exporter) Start(ctx context.Context, addr string) error {
	logger := log.FromContext(ctx).WithName(ControllerName)

	//if err := prometheus.Register(e.collector); err != nil {
	//	return fmt.Errorf("failed to register metrics: %w", err)
	//}
	if err := e.registry.Register(e.collector); err != nil {
		return fmt.Errorf("failed to register metrics: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(e.registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", e.healthHandler)

	e.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		logger.Info("Starting metrics server", "addr", addr)
		if err := e.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(err, "Failed to start metrics server")
		}
	}()

	go e.runGatheringMetricsLoop(ctx)

	return nil
}

// Stop stops the SLURM metrics exporter
func (e *Exporter) Stop(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName(ControllerName)
	logger.Info("Stopping metrics exporter")

	close(e.stopCh) // Signal the collection loop to stop

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := e.httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shutdown HTTP server: %w", err)
	}

	logger.Info("Metrics exporter stopped")
	return nil
}

// runGatheringMetricsLoop gathers metrics from SLURM and stores it in-memory.
func (e *Exporter) runGatheringMetricsLoop(ctx context.Context) {
	logger := log.FromContext(ctx).WithName(ControllerName)
	ticker := time.NewTicker(e.params.ScrapeInterval)

	logger.Info("Starting metrics gathering", "interval", e.params.ScrapeInterval)

	// Gather metrics immediately on startup
	if err := e.gatherMetrics(ctx); err != nil {
		logger.Error(err, "Failed to gatherMetrics metrics")
	}

	for {
		select {
		case <-ticker.C:
			if err := e.gatherMetrics(ctx); err != nil {
				logger.Error(err, "Failed to gatherMetrics metrics")
			}
		case <-e.stopCh:
			logger.Info("Stopping metrics gathering")
			return
		case <-ctx.Done():
			logger.Info("Context canceled, stopping metrics gathering")
			return
		}
	}
}

// gatherMetrics collects metrics from SLURM and updates the Prometheus metrics
func (e *Exporter) gatherMetrics(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName(ControllerName)
	logger.V(1).Info("Gathering metrics from SLURM API")

	timeout := min(e.params.ScrapeInterval/2, 30*time.Second)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, err := e.slurmAPIClient.ListNodes(ctx)
	if err != nil {
		return err
	}

	logger.V(1).Info("Metrics gathering completed")
	return nil
}

// healthHandler handles health check requests
func (e *Exporter) healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("healthy"))
}
