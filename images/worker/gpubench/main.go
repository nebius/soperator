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
	"time"

	"github.com/sirupsen/logrus"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const (
	gpuBenchmarkFinished = "GPUBenchmarkFinished"
	gpuBenchmarkExecuted = "GPUBenchmarkExecuted"
	logFormatText        = "text"
	logFormatJSON        = "json"
)

var (
	err error
	log *logrus.Entry

	nameNCCL    = "nccl-benchmark"
	currentNode string

	fs                  = flag.NewFlagSet("flag", flag.ExitOnError)
	minBytes            = fs.String("min_bytes", "512M", "minimum size to start with")
	maxBytes            = fs.String("max_bytes", "8G", "maximum size to end at")
	stepFactor          = fs.Int("step_factor", 2, "multiplication factor between sizes")
	limit               = fs.Float64("limit", 420, "limit")
	useInfiniband       = fs.Bool("use_infiniband", false, "use infiniband for NCCL")
	drainSlurmNode      = fs.Bool("drain_state", false, "drain slurm node")
	namespace           = fs.String("namespace", "default", "kubernetes namespace")
	k8sServiceHost      = fs.String("kube_service_host", "kubernetes.default.svc", "kubernetes kube apiserver host")
	k8sServicePort      = fs.Int("kube_service_port", 443, "kubernetes kube apiserver port")
	pushEvents          = fs.Bool("push_events", false, "push events to kubernetes")
	pushMetricsGrpc     = fs.Bool("push_metrics_grpc", false, "push grpc metrics to opentelemetry")
	pushMetricsHttp     = fs.Bool("push_metrics_http", false, "push http metrics to opentelemetry")
	pushMetricsInsecure = fs.Bool("push_metrics_insecure", true, "push metrics insecure")
	pushMetricsPath     = fs.String("push_metrics_path", "/v1/metrics", "push metrics path")
	pushMetricsRetry    = fs.Bool("push_metrics_retry", false, "push metrics retry")
	exporterEndpoint    = fs.String("exporter_endpoint", "localhost:4317", "opentelemetry exporter endpoint")
	debugLog            = fs.Bool("debug", false, "debug log")
	logFormat           = fs.String("log_format", logFormatJSON, "log format (text or json)")
)

func init() {
	if *logFormat != logFormatText && *logFormat != logFormatJSON {
		logrus.WithField("logFormat", *logFormat).Fatal("Invalid log format")
	}

	if *logFormat == logFormatText {
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
	_ = fs.Parse(os.Args[1:])
	log.Info(fmt.Sprintf("Starting %s", nameNCCL))
	if *debugLog {
		debugFlags()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	meter := otel.Meter(nameNCCL)

	if *pushMetricsGrpc || *pushMetricsHttp {
		shutdownMeterProvider, err := initMeterProvider(ctx)
		if err != nil {
			log.Fatal(err)
		}
		defer func() {
			if err := shutdownMeterProvider(ctx); err != nil {
				log.Fatalf("failed to shutdown MeterProvider: %s", err)
			}
		}()
	}
	var eventGenerator *EventGenerator
	if *pushEvents {
		eventGenerator = NewEventGenerator(k8sServiceHost, k8sServicePort)
	}

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
		if *pushEvents {
			eventGenerator.generateEvent(ctx, currentNode, failExecuteMsg, v1.EventTypeWarning, gpuBenchmarkFinished)
		}
		log.WithFields(
			logrus.Fields{
				"error":  err,
				"output": string(output),
			},
		).Fatal(failExecuteMsg)
	}
	succedExuteMsg := "Succed to execute all_reduce_perf"
	if *pushEvents {
		eventGenerator.generateEvent(ctx, currentNode, succedExuteMsg, v1.EventTypeNormal, gpuBenchmarkExecuted)
	}
	log.Info(succedExuteMsg)

	perfOutput := string(output)
	log.Debug(perfOutput)

	lines := strings.Split(perfOutput, "\n")
	avgBandwidth := getAvgBandwidth(ctx, eventGenerator, lines)

	if avgBandwidth < *limit {
		succeed := 0
		log.WithField("avg_bandwidth", avgBandwidth).Info(fmt.Sprintf("Avg bus bandwidth: %f", avgBandwidth))
		messageReason := fmt.Sprintf(
			"Soperator healthcheck: NCCL test all_reduce_perf: Avg bus bandwidth=%fGB/s, min=%fGB/s",
			avgBandwidth,
			*limit,
		)
		if *drainSlurmNode {
			drainNode(ctx, eventGenerator, currentNode, messageReason)
		}
		sendMetrics(ctx, meter, currentNode, avgBandwidth, *limit, succeed)
	} else {
		succeed := 1
		log.WithField("avg_bandwidth", avgBandwidth).Info(fmt.Sprintf(
			"Avg bus bandwidth > %f, min = %f",
			avgBandwidth,
			*limit))
		benchmarkFinishedMsg := fmt.Sprintf("GPU benchmark finished with Avg bus bandwidth=%f", avgBandwidth)
		log.WithField("avg_bandwidth", avgBandwidth).Info(benchmarkFinishedMsg)
		sendMetrics(ctx, meter, currentNode, avgBandwidth, *limit, succeed)
		if *pushEvents {
			eventGenerator.generateEvent(ctx, currentNode, benchmarkFinishedMsg, v1.EventTypeNormal, gpuBenchmarkFinished)
		}
	}
}

func debugFlags() {
	log.Debugf("min_bytes: %s", *minBytes)
	log.Debugf("max_bytes: %s", *maxBytes)
	log.Debugf("step_factor: %d", *stepFactor)
	log.Debugf("limit: %f", *limit)
	log.Debugf("use_infiniband: %v", *useInfiniband)
	log.Debugf("drain_state: %v", *drainSlurmNode)
	log.Debugf("namespace: %s", *namespace)
	log.Debugf("kube_service_host: %s", *k8sServiceHost)
	log.Debugf("kube_service_port: %d", *k8sServicePort)
	log.Debugf("push_events: %v", *pushEvents)
	log.Debugf("push_metrics_grpc: %v", *pushMetricsGrpc)
	log.Debugf("push_metrics_http: %v", *pushMetricsHttp)
	log.Debugf("push_metrics_insecure: %v", *pushMetricsInsecure)
	log.Debugf("push_metrics_path: %s", *pushMetricsPath)
	log.Debugf("push_metrics_retry: %v", *pushMetricsRetry)
	log.Debugf("exporter_endpoint: %s", *exporterEndpoint)
	log.Debugf("debug: %v", *debugLog)
	log.Debugf("log_format: %s", *logFormat)
}

type KubernetesClient interface {
	CoreV1() corev1.CoreV1Interface
}

type EventGenerator struct {
	clientset      KubernetesClient
	onceClientset  sync.Once
	k8sServiceHost *string
	k8sServicePort *int
}

func NewEventGenerator(k8sServiceHost *string, k8sServicePort *int) *EventGenerator {
	return &EventGenerator{
		k8sServiceHost: k8sServiceHost,
		k8sServicePort: k8sServicePort,
	}
}

func (e *EventGenerator) initClientset() (*kubernetes.Clientset, error) {
	var config *rest.Config

	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".kube", "config")); os.IsNotExist(err) {
		// In-cluster config
		os.Setenv("KUBERNETES_SERVICE_HOST", *e.k8sServiceHost)
		os.Setenv("KUBERNETES_SERVICE_PORT", strconv.Itoa(*e.k8sServicePort))
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	} else {
		// Out-of-cluster config
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
	}

	return kubernetes.NewForConfig(config)
}

func (e *EventGenerator) generateEvent(ctx context.Context, currentNode, message, eventType, reason string) {
	if *pushEvents {
		e.onceClientset.Do(func() {
			var err error
			e.clientset, err = e.initClientset()
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

		event, err := e.clientset.CoreV1().Events(*namespace).Create(ctx, event, opts)
		if err != nil {
			log.WithField("error", err).Error("Failed to create event")
		}

		log.WithField("event", event).Debug("Event created")
	}
}

func getAvgBandwidth(ctx context.Context, e *EventGenerator, lines []string) float64 {
	var avgBandwidth float64

	foundLine := false
	for _, line := range lines {
		if strings.Contains(line, "Avg bus bandwidth") {
			parts := strings.Fields(line)
			avgBandwidth, err = strconv.ParseFloat(parts[len(parts)-1], 64)
			if err != nil {
				noOutput := "No AVG bandwidth output, test in trouble"
				e.generateEvent(ctx, currentNode, noOutput, v1.EventTypeWarning, "NoAVGBandwidthOutput")
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

func drainNode(ctx context.Context, e *EventGenerator, slurmNode, messageReason string) {
	cmd := exec.Command("scontrol", "update", "NodeName="+currentNode, "State=drain", fmt.Sprintf("Reason=%q", messageReason))
	_, err := cmd.CombinedOutput()
	if err != nil {
		failedDrainNodeMsg := fmt.Sprintf("Failed to drain node %s", slurmNode)
		e.generateEvent(ctx, slurmNode, failedDrainNodeMsg, v1.EventTypeWarning, "FailedDrainSlurmNode")
		log.WithField("error", err).Fatal(failedDrainNodeMsg)
	}
	log.WithField("node", slurmNode).Info("Node drained with reason: ", messageReason)
}

func initMeterProvider(ctx context.Context) (func(context.Context) error, error) {
	res, err := generateMetricsResource(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	if *pushMetricsGrpc {
		conn, err := initGRPCConn()
		if err != nil {
			return nil, fmt.Errorf("failed to create gRPC connection: %w", err)
		}
		return initGRPCMeterProvider(ctx, res, conn)
	} else if *pushMetricsHttp {
		return initHTTPMeterProvider(ctx, res)
	}
	return nil, fmt.Errorf("no metrics provider selected")
}

func generateMetricsResource(ctx context.Context) (*resource.Resource, error) {
	serviceName := semconv.ServiceNameKey.String(nameNCCL)
	return resource.New(ctx,
		resource.WithAttributes(
			serviceName,
		),
	)
}

func initGRPCConn() (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{}
	if *pushMetricsInsecure {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	conn, err := grpc.NewClient(*exporterEndpoint, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to collector: %w", err)
	}
	return conn, err
}

func initGRPCMeterProvider(ctx context.Context, res *resource.Resource, conn *grpc.ClientConn) (func(context.Context) error, error) {
	otps := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithGRPCConn(conn),
		otlpmetricgrpc.WithRetry(
			otlpmetricgrpc.RetryConfig{
				Enabled:         *pushMetricsRetry,
				InitialInterval: 5 * time.Second,
				MaxInterval:     30 * time.Second,
				MaxElapsedTime:  time.Minute,
			},
		),
	}

	metricExporter, err := otlpmetricgrpc.New(ctx, otps...)
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

func initHTTPMeterProvider(ctx context.Context, res *resource.Resource) (func(context.Context) error, error) {
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(*exporterEndpoint),
		otlpmetrichttp.WithURLPath(*pushMetricsPath),
		otlpmetrichttp.WithRetry(
			otlpmetrichttp.RetryConfig{
				Enabled:         *pushMetricsRetry,
				InitialInterval: 5 * time.Second,
				MaxInterval:     30 * time.Second,
				MaxElapsedTime:  time.Minute,
			},
		),
	}

	if *pushMetricsInsecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}

	metricExporter, err := otlpmetrichttp.New(ctx, opts...)
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

func sendMetrics(ctx context.Context, meter metric.Meter, slurmNode string, avgBandwidth, limitValue float64, succeed int) {
	if *pushMetricsGrpc || *pushMetricsHttp {
		log.WithField("avg_bandwidth", avgBandwidth).Debug("Sending metrics")
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
