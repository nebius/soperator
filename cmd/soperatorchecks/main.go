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
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/cli"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/soperatorchecks"
	"nebius.ai/slurm-operator/internal/controllersenabled"
	"nebius.ai/slurm-operator/internal/slurmapi"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
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
	utilruntime.Must(kruisev1b1.AddToScheme(scheme))
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
		metricsAddr                 string
		enableLeaderElection        bool
		probeAddr                   string
		secureMetrics               bool
		enableHTTP2                 bool
		logFormat                   string
		logLevel                    string
		enabledNodeReplacement      bool
		enableExtensiveCheck        bool
		deleteNotReadyNodes         bool
		notReadyTimeout             time.Duration
		maintenanceConditionType    string
		maintenanceIgnoreNodeLabels string
		controllersFlag             string

		reconcileTimeout                         time.Duration
		reconcileTimeoutPodEphemeralStorageCheck time.Duration
		maxConcurrency                           int
		maxConcurrencyPodEphemeralStorageCheck   int
		cacheSyncTimeout                         time.Duration
		ephemeralStorageThreshold                float64
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
	flag.DurationVar(&reconcileTimeout, "reconcile-timeout", 3*time.Minute, "The maximum duration allowed for a single reconcile")
	flag.DurationVar(&reconcileTimeoutPodEphemeralStorageCheck, "pod-ephemeral-reconcile-timeout", 15*time.Second, "The maximum duration allowed for a single reconcile of Pod Ephemeral Storage Check")
	flag.IntVar(&maxConcurrency, "max-concurrent-reconciles", 1, "Configures number of concurrent reconciles. It should improve performance for clusters with many objects.")
	flag.IntVar(&maxConcurrencyPodEphemeralStorageCheck, "pod-ephemeral-max-concurrent-reconciles", 10, "Configures number of concurrent reconciles for Pod Ephemeral Storage Check. It should improve performance for clusters with many pods.")
	flag.DurationVar(&cacheSyncTimeout, "cache-sync-timeout", 2*time.Minute, "The maximum duration allowed for caching sync")
	flag.BoolVar(&enabledNodeReplacement, "enable-node-replacement", true, "Enable node replacement controller")
	flag.BoolVar(&enableExtensiveCheck, "enable-extensive-check", true, "If set, runs extensive check before setting unhealthy flag for HC failures")
	flag.DurationVar(&notReadyTimeout, "not-ready-timeout", 15*time.Minute, "The timeout after which a NotReady node will be deleted. Nodes can be NotReady for more than 10 minutes when GPU operator is starting.")
	flag.BoolVar(&deleteNotReadyNodes, "delete-not-ready-nodes", true, "If set, NotReady nodes will be deleted after the not-ready timeout is reached. If false, they will be marked as NotReady but not deleted.")
	flag.Float64Var(&ephemeralStorageThreshold, "ephemeral-storage-threshold", 85.0, "The threshold percentage for ephemeral storage usage warnings (default 85%)")
	flag.StringVar(&maintenanceConditionType, "maintenance-condition-type", string(consts.DefaultMaintenanceConditionType), "The condition type for scheduled maintenance")
	flag.StringVar(&maintenanceIgnoreNodeLabels, "maintenance-ignore-node-labels", os.Getenv("MAINTENANCE_IGNORE_NODE_LABELS"), "Comma-separated list of node label key=value pairs to ignore during maintenance (e.g., 'env=prod,tier=critical')")
	flag.StringVar(&controllersFlag, "controllers", "", "A comma-separated list of controllers to enable or disable. Use '*' for all, and '-name' to disable. Overrides SLURM_OPERATOR_CONTROLLERS if set.")
	flag.Parse()

	opts := getZapOpts(logFormat, logLevel)
	zapLogger := zap.New(opts...)
	ctrl.SetLogger(zapLogger)

	// Configure klog to use the same logger as controller-runtime
	// This ensures that leader election logs are in the same format
	klog.SetLogger(zapLogger.WithName("klog"))

	setupLog := ctrl.Log.WithName("setup")
	controllersSpec := os.Getenv("SLURM_OPERATOR_CONTROLLERS")
	controllersSource := "env"
	if controllersFlag != "" {
		controllersSpec = controllersFlag
		controllersSource = "flag"
	}
	availableControllers := []string{
		"slurmapiclients",
		"slurmnodes",
		"k8snodes",
		"activecheck",
		"activecheckjob",
		"serviceaccount",
		"activecheckprolog",
		"podephemeralstoragecheck",
	}
	controllersSet, err := controllersenabled.New(
		controllersSpec,
		availableControllers,
	)
	if err != nil {
		cli.Fail(setupLog, err, "unable to parse SLURM_OPERATOR_CONTROLLERS")
	}
	if controllersSpec != "" {
		for _, name := range availableControllers {
			if !controllersSet.Enabled(name) {
				setupLog.Info("controller disabled", "controller", name, "source", controllersSource)
			}
		}
	}

	// Validate ephemeral storage threshold
	if ephemeralStorageThreshold < 0 || ephemeralStorageThreshold > 100 {
		cli.Fail(setupLog, errors.New("invalid threshold"), fmt.Sprintf("ephemeral-storage-threshold must be between 0 and 100, got: %.2f", ephemeralStorageThreshold))
	}

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
		cli.Fail(setupLog, err, "unable to start manager")
	}

	ctx := context.Background()

	// Index pods by node name. This is used to list and evict pods from a specific node.
	if err = mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, "spec.nodeName", func(rawObj client.Object) []string {
		pod := rawObj.(*corev1.Pod)
		return []string{pod.Spec.NodeName}
	}); err != nil {
		cli.Fail(setupLog, err, "unable to setup index field")
	}

	slurmAPIClients := slurmapi.NewClientSet()

	if controllersSet.Enabled("slurmapiclients") {
		if err = soperatorchecks.NewSlurmAPIClientsController(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetEventRecorderFor(soperatorchecks.SlurmAPIClientsControllerName),
			slurmAPIClients,
			corev1.NodeConditionType(maintenanceConditionType),
		).SetupWithManager(mgr, maxConcurrency, cacheSyncTimeout); err != nil {
			cli.Fail(setupLog, err, "unable to create slurm api clients controller", "controller", soperatorchecks.SlurmAPIClientsControllerName)
		}
	}
	if controllersSet.Enabled("slurmnodes") {
		if err = soperatorchecks.NewSlurmNodesController(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetEventRecorderFor(soperatorchecks.SlurmNodesControllerName),
			slurmAPIClients,
			reconcileTimeout,
			enabledNodeReplacement,
			enableExtensiveCheck,
			mgr.GetAPIReader(),
			corev1.NodeConditionType(maintenanceConditionType),
		).SetupWithManager(mgr, maxConcurrency, cacheSyncTimeout); err != nil {
			cli.Fail(setupLog, err, "unable to create slurm nodes controller", "controller", soperatorchecks.SlurmNodesControllerName)
		}
	}
	if controllersSet.Enabled("k8snodes") {
		if err = soperatorchecks.NewK8SNodesController(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetEventRecorderFor(soperatorchecks.K8SNodesControllerName),
			notReadyTimeout,
			deleteNotReadyNodes,
			corev1.NodeConditionType(maintenanceConditionType),
			maintenanceIgnoreNodeLabels,
		).SetupWithManager(mgr, maxConcurrency, cacheSyncTimeout); err != nil {
			cli.Fail(setupLog, err, "unable to create k8s nodes controller", "controller", soperatorchecks.K8SNodesControllerName)
		}
	}
	if controllersSet.Enabled("activecheck") {
		if err = soperatorchecks.NewActiveCheckController(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetEventRecorderFor(soperatorchecks.SlurmActiveCheckControllerName),
			reconcileTimeout,
		).SetupWithManager(mgr, maxConcurrency, cacheSyncTimeout); err != nil {
			cli.Fail(setupLog, err, "unable to create activecheck controller", "controller", "ActiveCheck")
		}
	}
	if controllersSet.Enabled("activecheckjob") {
		if err = soperatorchecks.NewActiveCheckJobController(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetEventRecorderFor(soperatorchecks.SlurmActiveCheckJobControllerName),
			slurmAPIClients,
			reconcileTimeout,
		).SetupWithManager(mgr, maxConcurrency, cacheSyncTimeout); err != nil {
			cli.Fail(setupLog, err, "unable to create activecheckjob controller", "controller", "ActiveCheckJob")
		}
	}
	if controllersSet.Enabled("serviceaccount") {
		if err = soperatorchecks.NewServiceAccountController(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetEventRecorderFor(soperatorchecks.SlurmChecksServiceAccountControllerName),
			reconcileTimeout,
		).SetupWithManager(mgr, maxConcurrency, cacheSyncTimeout); err != nil {
			cli.Fail(setupLog, err, "unable to create soperatorchecks serviceaccount controller", "controller", "ServiceAccount")
		}
	}
	if controllersSet.Enabled("activecheckprolog") {
		if err = soperatorchecks.NewActiveCheckPrologController(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetEventRecorderFor(soperatorchecks.SlurmActiveCheckPrologControllerName),
			reconcileTimeout,
		).SetupWithManager(mgr, maxConcurrency, cacheSyncTimeout); err != nil {
			cli.Fail(setupLog, err, "unable to create soperatorchecks prolog controller", "controller", "Prolog")
		}
	}

	if controllersSet.Enabled("podephemeralstoragecheck") {
		podEphemeralStorageCheck, err := soperatorchecks.NewPodEphemeralStorageCheck(
			mgr.GetClient(),
			mgr.GetScheme(),
			mgr.GetEventRecorderFor(soperatorchecks.PodEphemeralStorageCheckName),
			ctrl.GetConfigOrDie(),
			reconcileTimeoutPodEphemeralStorageCheck,
			ephemeralStorageThreshold,
			slurmAPIClients,
		)
		if err != nil {
			cli.Fail(setupLog, err, "unable to create pod ephemeral storage check", "controller", "PodEphemeralStorageCheck")
		}
		if err = podEphemeralStorageCheck.SetupWithManager(mgr, maxConcurrencyPodEphemeralStorageCheck, cacheSyncTimeout); err != nil {
			cli.Fail(setupLog, err, "unable to setup pod ephemeral storage check", "controller", "PodEphemeralStorageCheck")
		}
	}

	//+kubebuilder:scaffold:builder

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
