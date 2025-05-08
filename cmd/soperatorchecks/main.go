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
	"context"
	"crypto/tls"
	"flag"
	"os"
	"time"

	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/controller/soperatorchecks"
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

		reconcileTimeout time.Duration
		maxConcurrency   int
		cacheSyncTimeout time.Duration
	)

	var watchNsCacheByName = make(map[string]cache.Config)

	ns := os.Getenv("SLURM_OPERATOR_WATCH_NAMESPACES")
	if ns != "" && ns != "*" {
		watchNsCacheByName = make(map[string]cache.Config)
		watchNsCacheByName[ns] = cache.Config{}
	}

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
	flag.DurationVar(&reconcileTimeout, "reconcile-timeout", 5*time.Minute, "The maximum duration allowed for a single reconcile")
	flag.IntVar(&maxConcurrency, "max-concurrent-reconciles", 1, "Configures number of concurrent reconciles. It should improve performance for clusters with many objects.")
	flag.DurationVar(&cacheSyncTimeout, "cache-sync-timeout", 5*time.Minute, "The maximum duration allowed for caching sync")
	flag.Parse()

	opts := getZapOpts(logFormat, logLevel)
	ctrl.SetLogger(zap.New(opts...))
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
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "a22156dp.nebius.ai",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
		Cache: cache.Options{
			DefaultNamespaces: watchNsCacheByName,
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	slurmAPIServer := os.Getenv("SLURM_API_SERVER")
	if len(slurmAPIServer) == 0 {
		slurmAPIServer = "http://localhost:6820"
	}

	// TODO: init jwt controller
	slurmClusterName := types.NamespacedName{
		Namespace: "soperator",
		Name:      "soperator",
	}
	jwtToken := jwt.NewToken(mgr.GetClient()).For(slurmClusterName, "root").WithRegistry(jwt.NewTokenRegistry().Build())
	slurmapiClient, err := slurmapi.NewClient(slurmAPIServer, jwtToken, slurmapi.DefaultHTTPClient())
	if err != nil {
		os.Exit(1)
	}
	slurmapiClients := map[types.NamespacedName]slurmapi.Client{
		slurmClusterName: slurmapiClient,
	}

	ctx := context.Background()

	// Index pods by node name. This is used to list and evict pods from a specific node.
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, "spec.nodeName", func(rawObj client.Object) []string {
		pod := rawObj.(*corev1.Pod)
		return []string{pod.Spec.NodeName}
	}); err != nil {
		setupLog.Error(err, "unable to setup index field")
		os.Exit(1)
	}

	if err = soperatorchecks.NewSlurmNodesController(
		mgr.GetClient(),
		mgr.GetScheme(),
		mgr.GetEventRecorderFor(soperatorchecks.SlurmNodesControllerName),
		slurmapiClients,
		reconcileTimeout,
	).SetupWithManager(mgr, maxConcurrency, cacheSyncTimeout); err != nil {
		setupLog.Error(err, "unable to create controller", soperatorchecks.SlurmNodesControllerName)
		os.Exit(1)
	}
	if err = soperatorchecks.NewK8SNodesController(
		mgr.GetClient(),
		mgr.GetScheme(),
		mgr.GetEventRecorderFor(soperatorchecks.K8SNodesControllerName),
	).SetupWithManager(mgr, maxConcurrency, cacheSyncTimeout); err != nil {
		setupLog.Error(err, "unable to create controller", soperatorchecks.K8SNodesControllerName)
		os.Exit(1)
	}
	if err = soperatorchecks.NewActiveCheckController(
		mgr.GetClient(),
		mgr.GetScheme(),
		mgr.GetEventRecorderFor(soperatorchecks.SlurmActiveCheckControllerName),
		reconcileTimeout,
	).SetupWithManager(mgr, maxConcurrency, cacheSyncTimeout); err != nil {
		setupLog.Error(err, "unable to create activecheck controller", "controller", "ActiveCheck")
		os.Exit(1)
	}
	if err = soperatorchecks.NewActiveCheckJobController(
		mgr.GetClient(),
		mgr.GetScheme(),
		mgr.GetEventRecorderFor(soperatorchecks.SlurmActiveCheckJobControllerName),
		reconcileTimeout,
	).SetupWithManager(mgr, maxConcurrency, cacheSyncTimeout); err != nil {
		setupLog.Error(err, "unable to create activecheckjob controller", "controller", "ActiveCheckJob")
		os.Exit(1)
	}
	if err = soperatorchecks.NewServiceAccountController(
		mgr.GetClient(),
		mgr.GetScheme(),
		mgr.GetEventRecorderFor(soperatorchecks.SlurmChecksServiceAccountControllerName),
		reconcileTimeout,
	).SetupWithManager(mgr, maxConcurrency, cacheSyncTimeout); err != nil {
		setupLog.Error(err, "unable to create soperatorchecks serviceaccount controller", "controller", "ServiceAccount")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
