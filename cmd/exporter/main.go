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
	"syscall"
	"time"

	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"nebius.ai/slurm-operator/internal/exporter"
	"nebius.ai/slurm-operator/internal/jwt"
	"nebius.ai/slurm-operator/internal/slurmapi"
)

type Flags struct {
	logFormat          string
	logLevel           string
	metricsAddr        string
	monitoringAddr     string
	slurmAPIServer     string
	clusterNamespace   string
	clusterName        string
	collectionInterval string

	// modes
	kubeconfigPath string 
	standalone     bool

	// auth
	staticToken string
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

// simple issuer that returns a fixed token
type staticIssuer struct{ tok string }
func (s staticIssuer) Issue(_ context.Context) (string, error) { return s.tok, nil }

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

	// choose issuer: k8s → jwt; else static-token; else none
	var issuer interface{ Issue(context.Context) (string, error) }
	switch {
	case ctrlClient != nil:
		issuer = jwt.NewToken(ctrlClient).For(slurmClusterID, "root").WithRegistry(jwt.NewTokenRegistry().Build())
	case flags.staticToken != "":
		issuer = staticIssuer{tok: flags.staticToken}
	default:
		issuer = nil
	}

	slurmAPIClient, err := slurmapi.NewClient(flags.slurmAPIServer, issuer, slurmapi.DefaultHTTPClient())
	if err != nil {
		log.Error(err, "Failed to initialize Slurm API client")
		os.Exit(1)
	}

	interval, err := time.ParseDuration(flags.collectionInterval)
	if err != nil {
		log.Error(err, "Failed to parse collection interval")
		os.Exit(1)
	}

	clusterExporter := exporter.NewClusterExporter(
		slurmAPIClient,
		exporter.Params{
			SlurmAPIServer:     flags.slurmAPIServer,
			SlurmClusterID:     slurmClusterID,
			CollectionInterval: interval,
		},
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := clusterExporter.Start(ctx, flags.metricsAddr); err != nil {
		log.Error(err, "Failed to start metrics exporter")
		os.Exit(1)
	}
	if err := clusterExporter.StartMonitoring(ctx, flags.monitoringAddr); err != nil {
		log.Error(err, "Failed to start monitoring server")
		os.Exit(1)
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
