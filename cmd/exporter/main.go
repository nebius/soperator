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
	logFormat        string
	logLevel         string
	metricsAddr      string
	slurmAPIServer   string
	scrapeInterval   time.Duration
	clusterNamespace string
	clusterName      string
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
	flag.StringVar(&flags.logFormat, "log-format", "json", "Log format: plain or json")
	flag.StringVar(&flags.logLevel, "log-level", "debug", "Log level: debug, info, warn, error, dpanic, panic, fatal")
	flag.StringVar(&flags.metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&flags.slurmAPIServer, "slurm-api-server", "http://localhost:6820", "The address of the Slurm REST API server.")
	flag.DurationVar(&flags.scrapeInterval, "scrape-interval", 30*time.Second, "The interval between metric collection runs")
	flag.StringVar(&flags.clusterNamespace, "cluster-namespace", "soperator", "The namespace of the Slurm cluster")
	flag.StringVar(&flags.clusterName, "cluster-name", "soperator", "The name of the Slurm cluster")
	flag.Parse()
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

	clusterExporter, err := exporter.NewClusterExporter(
		slurmAPIClient,
		exporter.Params{
			SlurmAPIServer: flags.slurmAPIServer,
			ScrapeInterval: flags.scrapeInterval,
			SlurmClusterID: slurmClusterID,
		},
	)
	if err != nil {
		log.Error(err, "Failed to create metrics exporter")
		os.Exit(1)
	}

	// Handle graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := clusterExporter.Start(ctx, flags.metricsAddr); err != nil {
		log.Error(err, "Failed to start metrics exporter")
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
