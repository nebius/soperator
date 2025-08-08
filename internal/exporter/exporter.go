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
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	// CollectionInterval specifies how often to collect metrics from SLURM APIs
	CollectionInterval time.Duration
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
	// monitoringRegistry is the Prometheus registry for self-monitoring metrics
	monitoringRegistry *prometheus.Registry
	// monitoringMetrics contains self-monitoring metrics
	monitoringMetrics *MonitoringMetrics
	// monitoringServer is the HTTP server for the monitoring endpoint
	monitoringServer *http.Server
}

// NewClusterExporter creates a new SLURM cluster exporter
func NewClusterExporter(slurmAPIClient slurmapi.Client, k8sClient client.Client, params Params) *Exporter {
	registry := prometheus.NewRegistry()
	monitoringRegistry := prometheus.NewRegistry()

	// ConfigMap name for state persistence
	configMapName := types.NamespacedName{
		Name:      "soperator-exporter-state",
		Namespace: params.SlurmClusterID.Namespace,
	}

	collector := NewMetricsCollector(slurmAPIClient, k8sClient, configMapName)

	return &Exporter{
		params:             params,
		slurmAPIClient:     slurmAPIClient,
		registry:           registry,
		collector:          collector,
		stopCh:             make(chan struct{}),
		monitoringRegistry: monitoringRegistry,
		monitoringMetrics:  collector.Monitoring,
	}
}

// Start starts the SLURM metrics exporter
func (e *Exporter) Start(ctx context.Context, addr string) error {
	logger := log.FromContext(ctx).WithName(ControllerName)

	if err := e.registry.Register(e.collector); err != nil {
		return fmt.Errorf("failed to register metrics: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", e.instrumentedMetricsHandler())
	mux.HandleFunc("/health", e.healthHandler)

	e.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go e.collectionLoop(ctx)

	go func() {
		logger.Info("Starting metrics server", "addr", addr)
		if err := e.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(err, "Failed to start metrics server")
		}
	}()

	return nil
}

// collectionLoop runs in the background and periodically collects metrics
func (e *Exporter) collectionLoop(ctx context.Context) {
	logger := log.FromContext(ctx).WithName(ControllerName)
	logger.Info("Starting metrics collection loop", "interval", e.params.CollectionInterval)

	ticker := time.NewTicker(e.params.CollectionInterval)
	defer ticker.Stop()

	if err := e.collector.updateState(ctx); err != nil {
		logger.Error(err, "Initial metrics collection failed")
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("Stopping metrics collection loop")
			return
		case <-e.stopCh:
			logger.Info("Stopping metrics collection loop via stop channel")
			return
		case <-ticker.C:
			if err := e.collector.updateState(ctx); err != nil {
				logger.Error(err, "Metrics collection failed")
			}
		}
	}
}

// Stop stops the SLURM metrics exporter
func (e *Exporter) Stop(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName(ControllerName)
	logger.Info("Stopping metrics exporter")

	close(e.stopCh) // Signal the collection loop to stop

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var errs []error

	if err := e.httpServer.Shutdown(shutdownCtx); err != nil {
		errs = append(errs, fmt.Errorf("failed to shutdown HTTP server: %w", err))
	}

	if e.monitoringServer != nil {
		if err := e.monitoringServer.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("failed to shutdown monitoring server: %w", err))
		}
	}

	return errors.Join(errs...)
}

// healthHandler handles health check requests
func (e *Exporter) healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("healthy"))
}

// instrumentedMetricsHandler wraps the metrics handler to track requests
func (e *Exporter) instrumentedMetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.monitoringMetrics.RecordMetricsRequest()
		promhttp.HandlerFor(e.registry, promhttp.HandlerOpts{}).ServeHTTP(w, r)
	})
}

// StartMonitoring starts the monitoring server on a separate port
func (e *Exporter) StartMonitoring(ctx context.Context, addr string) error {
	logger := log.FromContext(ctx).WithName(ControllerName)

	if err := e.monitoringMetrics.Register(e.monitoringRegistry); err != nil {
		return fmt.Errorf("failed to register monitoring metrics: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(e.monitoringRegistry, promhttp.HandlerOpts{}))

	e.monitoringServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		logger.Info("Starting monitoring server", "addr", addr)
		if err := e.monitoringServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(err, "Failed to start monitoring server")
		}
	}()

	return nil
}
