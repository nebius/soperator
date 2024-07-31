package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const (
	gpuBenchmarkFinished = "GPUBenchmarkFinished"
	gpuBenchmarkExecuted = "GPUBenchmarkExecuted"
	LogFormatText        = "text"
	LogFormatJSON        = "json"
)

var (
	err error
	log *logrus.Entry

	nameNCCL    = "nccl-benchmark"
	currentNode string

	clientset     *kubernetes.Clientset
	onceClientset sync.Once

	fs               = flag.NewFlagSet("flag", flag.ExitOnError)
	minBytes         = fs.String("min_bytes", "512M", "minimum size to start with")
	maxBytes         = fs.String("max_bytes", "8G", "maximum size to end at")
	stepFactor       = fs.Int("step_factor", 2, "multiplication factor between sizes")
	limit            = fs.Float64("limit", 420, "limit")
	useInfiniband    = fs.Bool("use_infiniband", false, "use infiniband for NCCL")
	drainSlurmNode   = fs.Bool("drain_state", false, "drain slurm node")
	namespace        = fs.String("namespace", "default", "kubernetes namespace")
	k8sServiceHost   = fs.String("kube_service_host", "kubernetes.default.svc", "kubernetes kube apiserver host")
	k8sServicePort   = fs.Int("kube_service_port", 443, "kubernetes kube apiserver port")
	pushEvents       = fs.Bool("push_events", false, "push events to kubernetes")
	pushMetricsGrpc  = fs.Bool("push_metrics_grpc", false, "push metrics to opentelemetry")
	exporterEndpoint = fs.String("exporter_endpoint", "localhost:4317", "opentelemetry exporter endpoint")
	debugLog         = fs.Bool("debug", false, "debug log")
	logFormat        = fs.String("log_format", LogFormatJSON, "log format (text or json)")
)

func init() {
	_ = fs.Parse(os.Args[1:])

	if *logFormat == LogFormatText {
		logrus.SetFormatter(&logrus.TextFormatter{})
	} else {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	}

	currentNode, err = os.Hostname()
	if err != nil {
		logrus.WithField("error", err).Fatal("Failed to get hostname")
	}

	if *debugLog {
		logrus.SetLevel(logrus.DebugLevel)
	} else {
		logrus.SetLevel(logrus.InfoLevel)
	}
	log = logrus.WithField("slurmNode", currentNode)
}

func main() {
	log.Info(fmt.Sprintf("Starting %s", nameNCCL))
	if *debugLog {
		debugFlags()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if *useInfiniband {
		log.Debug("Using Infiniband for NCCL")
		os.Setenv("NCCL_P2P_DISABLE", "1")
		os.Setenv("NCCL_SHM_DISABLE", "1")
		os.Setenv("NCCL_ALGO", "Ring")
	}

	gpuCount := os.Getenv("SLURM_GPUS")
	if gpuCount == "" {
		log.Fatal("Empty SLURM_GPUS")
	}

	stepFactorStr := strconv.Itoa(*stepFactor) // Convert StepFactor to string for exec.Command

	log.Debug("Starting all_reduce_perf")
	cmd := exec.Command(
		"/usr/bin/all_reduce_perf",
		"-b", *minBytes,
		"-e", *maxBytes,
		"-f", stepFactorStr, // Use the converted StepFactorStr
		"-g", gpuCount,
	)

	log.Debug("Executing all_reduce_perf")
	output, err := cmd.CombinedOutput()
	if err != nil {
		failExecuteMsg := "Failed to execute all_reduce_perf"
		generateEvent(ctx, currentNode, failExecuteMsg, v1.EventTypeWarning, gpuBenchmarkFinished)
		log.WithField("error", err).Fatal(failExecuteMsg)
	}
	succedExuteMsg := "Succed to execute all_reduce_perf"
	generateEvent(ctx, currentNode, succedExuteMsg, v1.EventTypeNormal, gpuBenchmarkExecuted)
	log.Info(succedExuteMsg)

	perfOutput := string(output)
	log.Debug(perfOutput)

	lines := strings.Split(perfOutput, "\n")
	avgBandwidth := getAvgBandwidth(ctx, lines)

	if avgBandwidth < *limit {
		succeed := 0
		log.WithField("avg_bandwidth", avgBandwidth).Info(fmt.Sprintf("Avg bus bandwidth: %f", avgBandwidth))
		messageReason := fmt.Sprintf(
			"The GPU benchmark ended with an unsatisfactory result for the NCCL test all_reduce_perf: Avg bus bandwidth=%f, min=%f",
			avgBandwidth,
			*limit, // Use the converted limitStr
		)
		if *drainSlurmNode == true {
			drainNode(ctx, currentNode, messageReason)
		}
		sendMetrics(ctx, currentNode, avgBandwidth, *limit, succeed)
		generateEvent(ctx, currentNode, messageReason, v1.EventTypeWarning, gpuBenchmarkFinished)
		log.Fatal(messageReason)
	} else {
		succeed := 1
		log.WithField("avg_bandwidth", avgBandwidth).Info(fmt.Sprintf(
			"Avg bus bandwidth > %f: %f",
			*limit, // Use the converted limitStr
			avgBandwidth))
		benchmarkFinishedMsg := fmt.Sprintf("GPU benchmark finished with Avg bus bandwidth=%f", avgBandwidth)
		log.WithField("avg_bandwidth", avgBandwidth).Info(benchmarkFinishedMsg)
		sendMetrics(ctx, currentNode, avgBandwidth, *limit, succeed)
		generateEvent(ctx, currentNode, benchmarkFinishedMsg, v1.EventTypeNormal, gpuBenchmarkFinished)
	}
}

func debugFlags() {
	log.WithField("min_bytes", *minBytes).Debug("Flag min_bytes")
	log.WithField("max_bytes", *maxBytes).Debug("Flag max_bytes")
	log.WithField("step_factor", *stepFactor).Debug("Flag step_factor")
	log.WithField("limit", *limit).Debug("Flag limit")
	log.WithField("use_infiniband", *useInfiniband).Debug("Flag use_infiniband")
	log.WithField("drain_state", *drainSlurmNode).Debug("Flag drain_state")
	log.WithField("namespace", *namespace).Debug("Flag namespace")
	log.WithField("kube_service_host", *k8sServiceHost).Debug("Flag kube_service_host")
	log.WithField("kube_service_port", *k8sServicePort).Debug("Flag kube_service_port")
	log.WithField("push_events", *pushEvents).Debug("Flag push_events")
	log.WithField("push_metrics_grpc", *pushMetricsGrpc).Debug("Flag push_metrics_grpc")
	log.WithField("exporter_endpoint", *exporterEndpoint).Debug("Flag exporter_endpoint")
	log.WithField("debug", *debugLog).Debug("Flag debug")
}

func generateEvent(ctx context.Context, currentNode, message, eventType, reason string) {
	if *pushEvents == true {
		onceClientset.Do(func() {
			clientset, err = initClientset(k8sServiceHost, k8sServicePort)
			if err != nil {
				log.WithField("error", err).Fatal("Failed to create clientset")
			}
		})
		event := &v1.Event{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "slurm-node-",
				Namespace:    *namespace,
			},
			Reason:         reason,
			Message:        message,
			Type:           eventType,
			LastTimestamp:  metav1.Now(),
			Source:         v1.EventSource{Component: nameNCCL},
			InvolvedObject: v1.ObjectReference{Kind: "Pod", Namespace: *namespace, Name: currentNode},
		}

		opts := metav1.CreateOptions{}

		event, err := clientset.CoreV1().Events(*namespace).Create(ctx, event, opts)
		if err != nil {
			log.WithField("error", err).Error("Failed to create event")
		}

		log.WithField("event", event).Debug("Event created")
	}
}

func getAvgBandwidth(ctx context.Context, lines []string) float64 {
	var avgBandwidth float64

	foundLine := false
	for _, line := range lines {
		if strings.Contains(line, "Avg bus bandwidth") {
			parts := strings.Fields(line)
			avgBandwidth, err = strconv.ParseFloat(parts[len(parts)-1], 64)
			if err != nil {
				noOutput := "No AVG bandwidth output, test in trouble"
				generateEvent(ctx, currentNode, noOutput, v1.EventTypeWarning, "NoAVGBandwidthOutput")
				log.WithField("error", err).Fatal(noOutput)
			}
			foundLine = true
			break
		}
	}
	if !foundLine {
		log.Fatal("No AVG bandwidth output, test in trouble")
	}
	return avgBandwidth
}

func drainNode(ctx context.Context, slurmNode, messageReason string) {
	cmd := exec.Command("scontrol", "update", "NodeName="+currentNode, "State=drain", "Reason="+messageReason)
	_, err := cmd.CombinedOutput()
	if err != nil {
		failedDrainNodeMsg := fmt.Sprintf("Failed to drain node %s", slurmNode)
		generateEvent(ctx, slurmNode, failedDrainNodeMsg, v1.EventTypeWarning, "FailedDrainSlurmNode")
		log.WithField("error", err).Fatal(failedDrainNodeMsg)
	}
	log.WithField("node", slurmNode).Info("Node drained with reason: ", messageReason)
}

func initClientset(K8SServiceHost *string, K8SServicePort *int) (*kubernetes.Clientset, error) {
	var config *rest.Config

	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".kube", "config")); os.IsNotExist(err) {
		os.Setenv("KUBERNETES_SERVICE_HOST", *K8SServiceHost)
		os.Setenv("KUBERNETES_SERVICE_PORT", strconv.Itoa(*K8SServicePort))
		// In-cluster config
		config, err = rest.InClusterConfig()
		if err != nil {
			log.WithField("error", err).Fatal("Failed to get in-cluster config")
		}
	} else {
		// Out-of-cluster config
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			log.WithField("error", err).Fatal("Failed to get out-of-cluster config")
		}
	}

	return kubernetes.NewForConfig(config)
}

func sendMetrics(ctx context.Context, slurmNode string, avgBandwidth, limitValue float64, succeed int) {
	if *pushMetricsGrpc == true {
		log.WithField("avg_bandwidth", avgBandwidth).Debug("Sending metrics")

		serviceName := semconv.ServiceNameKey.String(nameNCCL)

		conn, err := initConn()
		if err != nil {
			log.Fatal(err)
		}

		res, err := resource.New(ctx,
			resource.WithAttributes(
				// The service name used to display traces in backends
				serviceName,
			),
		)
		if err != nil {
			log.Fatal(err)
		}

		meter := otel.Meter(nameNCCL)
		shutdownMeterProvider, err := initMeterProvider(ctx, res, conn)
		if err != nil {
			log.Fatal(err)
		}

		defer func() {
			if err := shutdownMeterProvider(ctx); err != nil {
				log.Fatalf("failed to shutdown MeterProvider: %s", err)
			}
		}()

		commonAttrs := []attribute.KeyValue{
			attribute.String("namespace", *namespace),
			attribute.String("slurm_node", slurmNode),
		}

		avgBandwidthGauge, err := meter.Float64Gauge("slurm_job_nccl_benchmark_avg_bandwidth", metric.WithDescription("Avg bus bandwidth"))
		if err != nil {
			log.WithField("error", err).Error("Failed to create job metric")
		}
		limitValueGauge, err := meter.Float64Gauge("slurm_job_nccl_benchmark_limit_value", metric.WithDescription("Limit value"))
		if err != nil {
			log.WithField("error", err).Error("Failed to create job metric")
		}
		succeedGauge, err := meter.Int64Gauge("slurm_job_nccl_benchmark_succeed", metric.WithDescription("Succeed jobs. 0 - failed, 1 - succeed"))
		if err != nil {
			log.WithField("error", err).Error("Failed to create job metric")
		}

		avgBandwidthGauge.Record(ctx, avgBandwidth, metric.WithAttributes(commonAttrs...))
		log.WithField("avg_bandwidth", avgBandwidth).Info("Metrics sent")

		limitValueGauge.Record(ctx, limitValue, metric.WithAttributes(commonAttrs...))
		log.WithField("limit_value", limitValue).Info("Metrics sent")

		succeedGauge.Record(ctx, int64(succeed), metric.WithAttributes(commonAttrs...))
		log.WithField("succeed", succeed).Info("Metrics sent")
	}
}

func initConn() (*grpc.ClientConn, error) {
	// It connects the OpenTelemetry Collector through local gRPC connection.
	conn, err := grpc.NewClient(*exporterEndpoint,
		// TODO: add tls options in a future.
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to collector: %w", err)
	}

	return conn, err
}

// Initializes an OTLP exporter, and configures the corresponding meter provider.
func initMeterProvider(ctx context.Context, res *resource.Resource, conn *grpc.ClientConn) (func(context.Context) error, error) {
	metricExporter, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics exporter: %w", err)
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(meterProvider)

	return meterProvider.Shutdown, nil
}
