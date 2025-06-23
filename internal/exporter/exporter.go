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
func NewClusterExporter(slurmAPIClient slurmapi.Client, params Params) *Exporter {
	registry := prometheus.NewRegistry()
	collector := NewMetricsCollector(slurmAPIClient)

	return &Exporter{
		params:         params,
		slurmAPIClient: slurmAPIClient,
		registry:       registry,
		collector:      collector,
		stopCh:         make(chan struct{}),
	}
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

	return nil
}

// Stop stops the SLURM metrics exporter
func (e *Exporter) Stop(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName(ControllerName)
	logger.Info("Stopping metrics exporter")

	close(e.stopCh) // Signal the collection loop to stop

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := e.httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shutdown HTTP server: %w", err)
	}

	logger.Info("Metrics exporter stopped")
	return nil
}

// healthHandler handles health check requests
func (e *Exporter) healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("healthy"))
}
