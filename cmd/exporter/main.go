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

	// flagConfig represents configuration for a single flag/env var pair
	type flagConfig struct {
		flagName   string
		envName    string
		defaultVal string
		usage      string
		target     *string
	}

	configs := []flagConfig{
		{
			flagName:   "log-format",
			envName:    "SLURM_EXPORTER_LOG_FORMAT",
			defaultVal: "json",
			usage:      "Log format: plain or json",
			target:     &flags.logFormat,
		},
		{
			flagName:   "log-level",
			envName:    "SLURM_EXPORTER_LOG_LEVEL",
			defaultVal: "debug",
			usage:      "Log level: debug, info, warn, error, dpanic, panic, fatal",
			target:     &flags.logLevel,
		},
		{
			flagName:   "metrics-bind-address",
			envName:    "SLURM_EXPORTER_METRICS_BIND_ADDRESS",
			defaultVal: ":8080",
			usage:      "The address the metric endpoint binds to.",
			target:     &flags.metricsAddr,
		},
		{
			flagName:   "monitoring-bind-address",
			envName:    "SLURM_EXPORTER_MONITORING_BIND_ADDRESS",
			defaultVal: ":8081",
			usage:      "The address the monitoring endpoint binds to.",
			target:     &flags.monitoringAddr,
		},
		{
			flagName:   "slurm-api-server",
			envName:    "SLURM_EXPORTER_SLURM_API_SERVER",
			defaultVal: "http://localhost:6820",
			usage:      "The address of the Slurm REST API server.",
			target:     &flags.slurmAPIServer,
		},
		{
			flagName:   "cluster-namespace",
			envName:    "SLURM_EXPORTER_CLUSTER_NAMESPACE",
			defaultVal: "soperator",
			usage:      "The namespace of the Slurm cluster",
			target:     &flags.clusterNamespace,
		},
		{
			flagName:   "cluster-name",
			envName:    "SLURM_EXPORTER_CLUSTER_NAME",
			defaultVal: "",
			usage:      "The name of the Slurm cluster (required)",
			target:     &flags.clusterName,
		},
		{
			flagName:   "collection-interval",
			envName:    "SLURM_EXPORTER_COLLECTION_INTERVAL",
			defaultVal: "30s",
			usage:      "How often to collect metrics from SLURM APIs",
			target:     &flags.collectionInterval,
		},
	}

	for _, cfg := range configs {
		flag.StringVar(cfg.target, cfg.flagName, cfg.defaultVal, cfg.usage)
	}

	flag.Parse()

	// Build a set of flags that were explicitly passed on the command line
	passedFlags := make(map[string]struct{})
	flag.Visit(func(f *flag.Flag) {
		passedFlags[f.Name] = struct{}{}
	})

	// Apply environment variables if CLI flags were not explicitly set
	// Priority: CLI flag > Environment variable > Default value
	for _, cfg := range configs {
		if _, passed := passedFlags[cfg.flagName]; !passed {
			if envVal := os.Getenv(cfg.envName); envVal != "" {
				*cfg.target = envVal
			}
		}
	}

	if flags.clusterName == "" {
		_, _ = fmt.Fprintf(os.Stderr, "Error: cluster-name is required (set via --cluster-name flag or SLURM_EXPORTER_CLUSTER_NAME env var)\n")
		flag.Usage()
		os.Exit(1)
	}

	return flags
}

func main() {
	flags := parseFlags()
	opts := getZapOpts(flags.logFormat, flags.logLevel)

	ctrl.SetLogger(zap.New(opts...))
	log := ctrl.Log.WithName("soperator-exporter")

	slurmClusterID := types.NamespacedName{
		Namespace: flags.clusterNamespace,
		Name:      flags.clusterName,
	}

	config := ctrl.GetConfigOrDie()

	ctrlClient, err := client.New(config, client.Options{})
	if err != nil {
		log.Error(err, "Failed to create Kubernetes client")
		os.Exit(1)
	}

	jwtTokenIssuer := jwt.NewToken(ctrlClient).For(slurmClusterID, "root").WithRegistry(
		jwt.NewTokenRegistry().Build())

	slurmAPIClient, err := slurmapi.NewClient(flags.slurmAPIServer, jwtTokenIssuer, slurmapi.DefaultHTTPClient())
	if err != nil {
		log.Error(err, "Failed to initialize Slurm API client")
		os.Exit(1)
	}

	collectionInterval, err := time.ParseDuration(flags.collectionInterval)
	if err != nil {
		log.Error(err, "Failed to parse collection interval")
		os.Exit(1)
	}

	clusterExporter := exporter.NewClusterExporter(
		slurmAPIClient,
		exporter.Params{
			SlurmAPIServer:     flags.slurmAPIServer,
			SlurmClusterID:     slurmClusterID,
			CollectionInterval: collectionInterval,
		},
	)

	// Handle graceful shutdown
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
