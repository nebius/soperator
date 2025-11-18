/*
Copyright 2024.

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
	"crypto/tls"
	"flag"
	"time"

	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/cli"
	"nebius.ai/slurm-operator/internal/controller/sconfigcontroller"
	"nebius.ai/slurm-operator/internal/jwt"
	"nebius.ai/slurm-operator/internal/slurmapi"
	//+kubebuilder:scaffold:imports
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	//+kubebuilder:scaffold:scheme

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(slurmv1.AddToScheme(scheme))
	utilruntime.Must(slurmv1alpha1.AddToScheme(scheme))
}

func getZapOpts(logFormat, logLevel string) []zap.Opts {
	var zapOpts []zap.Opts

	// Configure log format
	if logFormat == "json" {
		zapOpts = append(zapOpts, zap.UseDevMode(false))
	} else {
		zapOpts = append(zapOpts, zap.UseDevMode(true))
	}

	// Configure log level
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

func main() {
	var (
		metricsAddr          string
		enableLeaderElection bool
		probeAddr            string
		secureMetrics        bool
		enableHTTP2          bool
		logFormat            string
		logLevel             string
		jailPath             string
		clusterNamespace     string
		clusterName          string
		slurmAPIServer       string

		maxConcurrency          int
		cacheSyncTimeout        time.Duration
		reconfigurePollInterval time.Duration
		reconfigureWaitTimeout  time.Duration
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.StringVar(&logFormat, "log-format", "json", "Log format: plain or json")
	flag.StringVar(&logLevel, "log-level", "debug", "Log level: debug, info, warn, error, dpanic, panic, fatal")
	flag.IntVar(&maxConcurrency, "max-concurrent-reconciles", 1, "Configures number of concurrent reconciles. It should improve performance for clusters with many objects.")
	flag.DurationVar(&cacheSyncTimeout, "cache-sync-timeout", 1*time.Minute, "The maximum duration allowed for caching sync")
	flag.DurationVar(&reconfigurePollInterval, "reconfigure-poll-interval", 20*time.Second, "The interval for polling node restart status during reconfiguration")
	flag.DurationVar(&reconfigureWaitTimeout, "reconfigure-wait-timeout", 1*time.Minute, "The maximum time to wait for all nodes to restart during reconfiguration")
	flag.StringVar(&jailPath, "jail-path", "/mnt/jail", "Path where jail is mounted")
	flag.StringVar(&clusterNamespace, "cluster-namespace", "default", "Soperator cluster namespace")
	flag.StringVar(&clusterName, "cluster-name", "soperator", "Name of the soperator cluster controller")
	flag.StringVar(&slurmAPIServer, "slurmapiserver", "http://localhost:6820", "Address of the SlurmAPI")
	flag.Parse()
	opts := getZapOpts(logFormat, logLevel)
	zapLogger := zap.New(opts...)
	ctrl.SetLogger(zapLogger)

	// Configure klog to use the same logger as controller-runtime
	// This ensures that leader election logs are in the same format
	klog.SetLogger(zapLogger.WithName("klog"))

	setupLog := ctrl.Log.WithName("setup")

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer:                 webhookServer,
		HealthProbeBindAddress:        probeAddr,
		LeaderElection:                enableLeaderElection,
		LeaderElectionID:              "vqeyz6ae.nebius.ai",
		LeaderElectionReleaseOnCancel: true,
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				clusterNamespace: {},
			},
		},
		Client: ctrlclient.Options{
			Cache: &ctrlclient.CacheOptions{
				DisableFor: []ctrlclient.Object{
					&corev1.Secret{},
					&corev1.ConfigMap{},
				},
			},
		},
	})
	if err != nil {
		cli.Fail(setupLog, err, "unable to start manager")
	}

	jwtToken := jwt.NewToken(mgr.GetClient()).
		For(types.NamespacedName{
			Namespace: clusterNamespace,
			Name:      clusterName,
		}, "root").
		WithRegistry(jwt.NewTokenRegistry().Build())

	slurmAPIClient, err := slurmapi.NewClient(slurmAPIServer, jwtToken, slurmapi.DefaultHTTPClient())
	if err != nil {
		cli.Fail(setupLog, err, "unable to start Slurm API Client")
	}

	jailFs := &sconfigcontroller.PrefixFs{
		Prefix: jailPath,
	}

	if err = (sconfigcontroller.NewJailedConfigReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		slurmAPIClient,
		jailFs,
		reconfigurePollInterval,
		reconfigureWaitTimeout,
	)).SetupWithManager(mgr, maxConcurrency, cacheSyncTimeout); err != nil {
		cli.Fail(setupLog, err, "unable to create controller", "controller", "JailedConfig")
	}

	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		cli.Fail(setupLog, err, "unable to set up health check")
	}
	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		cli.Fail(setupLog, err, "unable to set up ready check")
	}

	setupLog.Info("starting manager")
	if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		cli.Fail(setupLog, err, "unable to start manager")
	}
}
