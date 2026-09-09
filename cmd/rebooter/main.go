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
	"fmt"
	"os"
	"time"

	"go.uber.org/zap/zapcore"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"nebius.ai/slurm-operator/internal/cli"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/kubeletclient"
	metricsopts "nebius.ai/slurm-operator/internal/metrics"
	"nebius.ai/slurm-operator/internal/rebooter"
	//+kubebuilder:scaffold:imports
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	//+kubebuilder:scaffold:scheme

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
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

// newNodePodsFetcher builds the fetcher used to check pod eviction during a drain.
// It reads from the node's own kubelet, keeping the API server off the critical path of a
// reboot, and falls back to the API server node proxy when the kubelet cannot be reached.
// Both paths ask the same kubelet, so the fallback cannot report a different answer.
func newNodePodsFetcher(config *rest.Config, kubeletConfig kubeletclient.Config) (rebooter.NodePodsFetcher, error) {
	kubeletFetcher, err := rebooter.NewKubeletNodePodsFetcher(config, kubeletConfig)
	if err != nil {
		return nil, fmt.Errorf("create kubelet node pods fetcher: %w", err)
	}

	proxyFetcher, err := rebooter.NewAPIServerNodePodsFetcher(config)
	if err != nil {
		return nil, fmt.Errorf("create API server node proxy fetcher: %w", err)
	}

	return rebooter.NewFallbackNodePodsFetcher(kubeletFetcher, proxyFetcher), nil
}

// maxConcurrency is the maximum number of concurrent reconciles for a controller.
// For reconsiling the node it has to be 1. Otherwise, it wold  be possible to get race conditions.
const maxConcurrency = 1

func main() {
	var (
		metricsAddr   string
		probeAddr     string
		secureMetrics bool
		enableHTTP2   bool
		logFormat     string
		logLevel      string

		reconcileTimeout time.Duration
		cacheSyncTimeout time.Duration

		kubeletPort                  int
		kubeletTimeout               time.Duration
		kubeletInsecureSkipTLSVerify bool
		kubeletCAFile                string
		kubeletTLSServerName         string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.StringVar(&logFormat, "log-format", "json", "Log format: plain or json")
	flag.StringVar(&logLevel, "log-level", "debug", "Log level: debug, info, warn, error, dpanic, panic, fatal")
	flag.DurationVar(&reconcileTimeout, "reconcile-timeout", 1*time.Minute, "The maximum duration allowed for a single reconcile if some error occurs")
	flag.DurationVar(&cacheSyncTimeout, "cache-sync-timeout", 2*time.Minute, "The maximum duration allowed for caching sync")
	flag.IntVar(&kubeletPort, "kubelet-port", kubeletclient.DefaultPort, "The kubelet port used on nodes that do not advertise a kubelet endpoint.")
	flag.DurationVar(&kubeletTimeout, "kubelet-request-timeout", 10*time.Second, "The timeout for a single request to the local kubelet.")
	flag.BoolVar(&kubeletInsecureSkipTLSVerify, "kubelet-insecure-skip-tls-verify", true, "If set, the kubelet serving certificate is not verified. Kubelet serving certificates are self-signed unless the cluster enables serverTLSBootstrap.")
	flag.StringVar(&kubeletCAFile, "kubelet-ca-file", "", "The CA bundle used to verify the kubelet serving certificate. Only used when kubelet-insecure-skip-tls-verify is false.")
	flag.StringVar(&kubeletTLSServerName, "kubelet-tls-server-name", "", "The server name used to verify the kubelet serving certificate. Only used when kubelet-insecure-skip-tls-verify is false.")
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

	nodeName := os.Getenv(consts.RebooterNodeNameEnv)
	if nodeName == "" {
		cli.Fail(setupLog, fmt.Errorf("%s environment variable is not set", consts.RebooterNodeNameEnv), "unable to determine rebooter node name")
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		// The rebooter runs on every node, so an unrestricted cache would hold the whole cluster in every pod.
		// The field selectors below are server-side: any informer this manager ever starts LISTs and WATCHes
		// only this pod's own node and the pods running on it.
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.Node{}: {Field: fields.OneTermEqualSelector("metadata.name", nodeName)},
				// Guardrail: nothing caches pods today (pod reads go straight to the local kubelet),
				// but a single cached pod read added in the future would silently start an informer over
				// every pod in the cluster. This selector caps such an informer to this node's own pods.
				&corev1.Pod{}: {Field: fields.OneTermEqualSelector("spec.nodeName", nodeName)},
			},
		},
		Metrics:                metricsopts.ServerOptions(metricsAddr, secureMetrics, tlsOpts),
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         false,
		LeaderElectionID:       "rebooter64591po.nebius.ai",
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
	})
	if err != nil {
		cli.Fail(setupLog, err, "unable to start manager")
	}

	rebooterParams := rebooter.RebooterParams{
		ReconcileTimeout: reconcileTimeout,
		NodeName:         nodeName,
	}

	envEvictionMethod := os.Getenv(consts.RebooterMethodEnv)
	switch envEvictionMethod {
	case string(consts.RebooterDrain):
		// TODO: Implement drain method
		cli.Fail(setupLog, fmt.Errorf("drain method is not supported"), "unable to start manager")
	case string(consts.RebooterEvict):
		fallthrough
	default:
		rebooterParams.EvictionMethod = consts.RebooterEvict
	}
	nodePodsFetcher, err := newNodePodsFetcher(mgr.GetConfig(), kubeletclient.Config{
		Port:                  int32(kubeletPort),
		Timeout:               kubeletTimeout,
		InsecureSkipTLSVerify: kubeletInsecureSkipTLSVerify,
		CAFile:                kubeletCAFile,
		TLSServerName:         kubeletTLSServerName,
	})
	if err != nil {
		cli.Fail(setupLog, err, "unable to create node pods fetcher")
	}

	if err = rebooter.NewRebooterReconciler(
		mgr.GetClient(),
		mgr.GetAPIReader(),
		mgr.GetScheme(),
		mgr.GetEventRecorderFor(rebooter.ControllerName),
		rebooterParams,
		nodePodsFetcher,
	).SetupWithManager(mgr, maxConcurrency, cacheSyncTimeout, rebooterParams.NodeName); err != nil {
		cli.Fail(setupLog, err, "unable to create controller", rebooter.ControllerName)
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
