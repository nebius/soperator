/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/common/model"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"nebius.ai/slurm-operator/internal/cli"
	"nebius.ai/slurm-operator/internal/exporter"
	"nebius.ai/slurm-operator/internal/jwt"
	"nebius.ai/slurm-operator/internal/slurmapi"
	tokenstandalone "nebius.ai/slurm-operator/internal/token-standalone"
)

type Flags struct {
	logFormat              string
	logLevel               string
	metricsAddr            string
	monitoringAddr         string
	slurmAPIServer         string
	clusterNamespace       string
	clusterName            string
	collectionInterval     string
	jobSource              string
	accountingJobStates    string
	accountingJobsLookback string

	// modes
	kubeconfigPath string
	standalone     bool

	// auth
	staticToken         string
	scontrolPath        string
	keyRotationInterval string
}

func getZapOpts(logFormat, logLevel string) []zap.Opts {
	var zapOpts []zap.Opts
	if logFormat == "json" {
		zapOpts = append(zapOpts, zap.UseDevMode(false))
	} else {
		zapOpts = append(zapOpts, zap.UseDevMode(true))
	}
	var level zapcore.Level
	switch logLevel {
	case "debug":
		level = zapcore.DebugLevel
	case "info":
		level = zapcore.InfoLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	case "dpanic":
		level = zapcore.DPanicLevel
	case "panic":
		level = zapcore.PanicLevel
	case "fatal":
		level = zapcore.FatalLevel
	default:
		level = zapcore.InfoLevel
	}
	zapOpts = append(zapOpts, zap.Level(level))
	return zapOpts
}

func parseFlags() Flags {
	var flags Flags

	type flagConfig struct {
		flagName   string
		envName    string
		defaultVal string
		usage      string
		target     *string
	}
	configs := []flagConfig{
		{"log-format", "SLURM_EXPORTER_LOG_FORMAT", "json", "Log format: plain or json", &flags.logFormat},
		{"log-level", "SLURM_EXPORTER_LOG_LEVEL", "debug", "Log level", &flags.logLevel},
		{"metrics-bind-address", "SLURM_EXPORTER_METRICS_BIND_ADDRESS", ":8080", "The address the metric endpoint binds to.", &flags.metricsAddr},
		{"monitoring-bind-address", "SLURM_EXPORTER_MONITORING_BIND_ADDRESS", ":8081", "The address the monitoring endpoint binds to.", &flags.monitoringAddr},
		{"slurm-api-server", "SLURM_EXPORTER_SLURM_API_SERVER", "http://localhost:6820", "The address of the Slurm REST API server.", &flags.slurmAPIServer},
		{"cluster-namespace", "SLURM_EXPORTER_CLUSTER_NAMESPACE", "soperator", "The namespace of the Slurm cluster", &flags.clusterNamespace},
		{"cluster-name", "SLURM_EXPORTER_CLUSTER_NAME", "", "The name of the Slurm cluster (required)", &flags.clusterName},
		{"collection-interval", "SLURM_EXPORTER_COLLECTION_INTERVAL", "30s", "How often to collect metrics from SLURM APIs", &flags.collectionInterval},
		{"job-source", "SLURM_EXPORTER_JOB_SOURCE", "controller", "EXPERIMENTAL: SLURM job source: controller (Slurm controller API) or accounting (Slurm accounting API)", &flags.jobSource},
		{"accounting-job-states", "SLURM_EXPORTER_ACCOUNTING_JOB_STATES", "", "EXPERIMENTAL: when --job-source=accounting, CSV of Slurm job states forwarded verbatim to the accounting state filter (e.g. RUNNING,PENDING). Empty = no state filter. Filter is applied by slurmdbd to the historical states a job held during the lookback window (sacct --state semantics) — not to the current state of returned jobs.", &flags.accountingJobStates},
		{"accounting-jobs-lookback", "SLURM_EXPORTER_ACCOUNTING_JOBS_LOOKBACK", "1h", "EXPERIMENTAL: when --job-source=accounting, the size of the time window queried from the accounting API ([now - lookback, now + 5m]).", &flags.accountingJobsLookback},
		{"scontrol-path", "SLURM_EXPORTER_SCONTROL_PATH", "scontrol", "Path to scontrol command for standalone mode", &flags.scontrolPath},
		{"key-rotation-interval", "SLURM_EXPORTER_KEY_ROTATION_INTERVAL", "30m", "Key rotation interval for standalone mode (e.g., 30m, 1h)", &flags.keyRotationInterval},
	}

	for _, cfg := range configs {
		flag.StringVar(cfg.target, cfg.flagName, cfg.defaultVal, cfg.usage)
	}
	flag.StringVar(&flags.kubeconfigPath, "kubeconfig-path", "", "Path to a kubeconfig for out-of-cluster use (optional)")
	flag.BoolVar(&flags.standalone, "standalone", false, "Run without Kubernetes (skip k8s client/JWT)")

	// static token (optional); can also come from SLURM_EXPORTER_TOKEN
	flag.StringVar(&flags.staticToken, "static-token", "", "Static JWT to send in X-SLURM-USER-TOKEN (use with rest_auth/jwt)")

	flag.Parse()
	passedFlags := make(map[string]struct{})
	flag.Visit(func(f *flag.Flag) { passedFlags[f.Name] = struct{}{} })
	for _, cfg := range configs {
		if _, passed := passedFlags[cfg.flagName]; !passed {
			if envVal := os.Getenv(cfg.envName); envVal != "" {
				*cfg.target = envVal
			}
		}
	}

	if flags.staticToken == "" {
		if v := os.Getenv("SLURM_EXPORTER_TOKEN"); v != "" {
			flags.staticToken = v
		}
	}

	if flags.clusterName == "" {
		_, _ = fmt.Fprintf(os.Stderr, "Error: --cluster-name (or SLURM_EXPORTER_CLUSTER_NAME) is required\n")
		flag.Usage()
		os.Exit(1)
	}
	return flags
}

// parseDuration accepts both Prometheus-style durations (y/w/d/h/m/s/ms — what the CRD's
// prometheusv1.Duration regex allows) and Go-style time.Duration syntax (fractional values like
// "0.5s" or "2h45m30.5s"). Prometheus is tried first because it's the documented CRD shape;
// Go-style is the historical exporter-flag shape and is kept for backward compatibility. The two
// formats only overlap on integer h/m/s/ms values where both yield the same result.
func parseDuration(s string) (time.Duration, error) {
	d, promErr := model.ParseDuration(s)
	if promErr == nil {
		return time.Duration(d), nil
	}
	if gd, err := time.ParseDuration(s); err == nil {
		return gd, nil
	}
	return 0, promErr
}

func buildJobListParams(flags Flags) (slurmapi.ListJobsParams, error) {
	rawSource := strings.TrimSpace(strings.ToLower(flags.jobSource))
	if rawSource == "" {
		rawSource = string(slurmapi.JobSourceController)
	}
	source := slurmapi.JobSource(rawSource)
	switch source {
	case slurmapi.JobSourceController, slurmapi.JobSourceAccounting:
	default:
		return slurmapi.ListJobsParams{}, fmt.Errorf("unsupported job source %q", flags.jobSource)
	}

	var states []string
	for _, s := range strings.Split(flags.accountingJobStates, ",") {
		if s = strings.TrimSpace(s); s != "" {
			states = append(states, s)
		}
	}

	// Parse the lookback only when the accounting source is selected. A stale or invalid value
	// in the env/CLI must not abort the exporter when the controller path is in use.
	var lookback time.Duration
	if source == slurmapi.JobSourceAccounting {
		if flags.accountingJobsLookback == "" {
			return slurmapi.ListJobsParams{}, fmt.Errorf("--accounting-jobs-lookback is required when --job-source=accounting")
		}
		var err error
		lookback, err = parseDuration(flags.accountingJobsLookback)
		if err != nil {
			return slurmapi.ListJobsParams{}, fmt.Errorf("parse --accounting-jobs-lookback: %w", err)
		}
		if lookback <= 0 {
			return slurmapi.ListJobsParams{}, fmt.Errorf("--accounting-jobs-lookback must be > 0 when --job-source=accounting")
		}
	}

	return slurmapi.ListJobsParams{
		Source:              source,
		AccountingJobStates: states,
		AccountingLookback:  lookback,
		// In soperator the K8s SlurmCluster CR name is the same as Slurm's `ClusterName`, so
		// reusing --cluster-name scopes the slurmdbd query to this deployment's jobs even when
		// the slurmdbd backs multiple Slurm clusters.
		AccountingCluster: flags.clusterName,
	}, nil
}

// simple issuer that returns a fixed token
type staticIssuer struct{ tok string }

func (s staticIssuer) Issue(_ context.Context) (string, error) { return s.tok, nil }

type issuer interface {
	Issue(ctx context.Context) (string, error)
}

func selectTokenIssuer(flags Flags, ctrlClient client.Client, slurmClusterID types.NamespacedName, log logr.Logger) issuer {
	var issuer issuer

	switch {
	case ctrlClient != nil:
		issuer = jwt.NewToken(ctrlClient).For(slurmClusterID, "root").WithRegistry(jwt.NewTokenRegistry().Build())
	case flags.standalone:
		standaloneIssuer := tokenstandalone.NewStandaloneTokenIssuer(slurmClusterID, "root").
			WithScontrolPath(flags.scontrolPath)
		// Parse and set rotation interval
		if rotationInterval, err := time.ParseDuration(flags.keyRotationInterval); err == nil {
			standaloneIssuer.WithRotationInterval(rotationInterval)
		} else {
			log.Error(err, "Failed to parse key rotation interval, using default", "interval", flags.keyRotationInterval)
		}

		issuer = standaloneIssuer
	case flags.staticToken != "":
		issuer = staticIssuer{tok: flags.staticToken}
	default:
		issuer = nil
	}

	return issuer
}

func main() {
	flags := parseFlags()
	opts := getZapOpts(flags.logFormat, flags.logLevel)
	ctrl.SetLogger(zap.New(opts...))
	log := ctrl.Log.WithName("soperator-exporter")

	slurmClusterID := types.NamespacedName{Namespace: flags.clusterNamespace, Name: flags.clusterName}

	// optional k8s
	var cfg *rest.Config
	var err error
	if !flags.standalone {
		cfg, err = rest.InClusterConfig()
		if err != nil && flags.kubeconfigPath != "" {
			log.Info("Failed to get in-cluster config, trying kubeconfig file", "kubeconfig", flags.kubeconfigPath, "error", err)
			cfg, err = clientcmd.BuildConfigFromFlags("", flags.kubeconfigPath)
			if err != nil {
				log.Info("Failed to load kubeconfig file, continuing without Kubernetes client", "kubeconfig", flags.kubeconfigPath, "error", err)
				cfg = nil
			} else {
				log.Info("Successfully loaded kubeconfig file")
			}
		} else if err != nil {
			log.Info("Failed to get in-cluster config, continuing without Kubernetes client", "error", err)
		}
	}
	var ctrlClient client.Client
	if cfg != nil {
		ctrlClient, err = client.New(cfg, client.Options{})
		if err != nil {
			log.Error(err, "Failed to create Kubernetes client, continuing in standalone mode")
			ctrlClient = nil
		}
	}

	// Select the appropriate token issuer
	issuer := selectTokenIssuer(flags, ctrlClient, slurmClusterID, log)

	slurmAPIClient, err := slurmapi.NewClient(flags.slurmAPIServer, issuer, slurmapi.DefaultHTTPClient())
	if err != nil {
		cli.Fail(log, err, "Failed to initialize Slurm API client")
	}

	interval, err := parseDuration(flags.collectionInterval)
	if err != nil {
		cli.Fail(log, err, "Failed to parse collection interval")
	}

	jobListParams, err := buildJobListParams(flags)
	if err != nil {
		cli.Fail(log, err, "Failed to parse job collection configuration")
	}

	clusterExporter := exporter.NewClusterExporter(
		slurmAPIClient,
		exporter.Params{
			SlurmAPIServer:     flags.slurmAPIServer,
			SlurmClusterID:     slurmClusterID,
			CollectionInterval: interval,
			JobListParams:      jobListParams,
		},
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if flags.standalone {
		log.Info("Using standalone token issuer", "scontrol_path", flags.scontrolPath, "rotation_interval", flags.keyRotationInterval)
	}

	if err = clusterExporter.Start(ctx, flags.metricsAddr); err != nil {
		cli.Fail(log, err, "Failed to start metrics exporter")
	}
	if err = clusterExporter.StartMonitoring(ctx, flags.monitoringAddr); err != nil {
		cli.Fail(log, err, "Failed to start monitoring server")
	}

	<-ctx.Done()
	log.Info("Shutdown signal received, stopping exporter...")

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := clusterExporter.Stop(stopCtx); err != nil {
		log.Error(err, "Failed to stop metrics exporter")
	}
	log.Info("Metrics exporter stopped gracefully")
}
