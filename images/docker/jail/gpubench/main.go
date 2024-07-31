package main

import (
	"context"
	"flag"
	"fmt"
	"log"
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
)

var (
	err error

	nameNCCL = "nccl-benchmark"

	onceMeter sync.Once
	meter     metric.Meter

	clientset     *kubernetes.Clientset
	onceClientset sync.Once

	stepFactor       = flag.Int("step_factor", 2, "multiplication factor between sizes")
	limit            = flag.Float64("limit", 420, "limit")
	minBytes         = flag.String("min_bytes", "512M", "minimum size to start with")
	maxBytes         = flag.String("max_bytes", "8G", "maximum size to end at")
	useInfiniband    = flag.String("use_infiniband", "", "use infiniband for NCCL")
	namespace        = flag.String("namespace", "default", "kubernetes n amespace")
	k8sServiceHost   = flag.String("service_host", "kubernetes.default.svc", "kubernetes kube apiserver host")
	k8sServicePort   = flag.String("service_port", "443", "kubernetes kube apiserver port")
	drainSlurmNode   = flag.Bool("drain_state", false, "drain slurm node")
	pushEvents       = flag.Bool("push_events", false, "push events to kubernetes")
	pushMetricsGrpc  = flag.Bool("push_metrics_grpc", false, "push metrics to opentelemetry")
	exporterEndpoint = flag.String("exporter_endpoint", "localhost:4317", "opentelemetry exporter endpoint")
)

func init() {
	logrus.SetFormatter(&logrus.JSONFormatter{})
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

func main() {
	logrus.Info(fmt.Sprintf("Starting %s", nameNCCL))

	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	currentNode, err := os.Hostname()
	if err != nil {
		logrus.WithField("error", err).Fatal("Failed to get hostname")
	}

	if *useInfiniband == "true" {
		os.Setenv("NCCL_P2P_DISABLE", "1")
		os.Setenv("NCCL_SHM_DISABLE", "1")
		os.Setenv("NCCL_ALGO", "Ring")
	}

	gpuCount := os.Getenv("SLURM_GPUS")
	if gpuCount == "" {
		logrus.Fatal("Empty SLURM_GPUS")
	}

	stepFactorStr := strconv.Itoa(*stepFactor) // Convert StepFactor to string for exec.Command

	cmd := exec.Command(
		"/usr/bin/all_reduce_perf",
		"-b", *minBytes,
		"-e", *maxBytes,
		"-f", stepFactorStr, // Use the converted StepFactorStr
		"-g", gpuCount,
	)

	output, err := cmd.Output()
	if err != nil {
		failExuteMsg := "Failed to execute all_reduce_perf"
		generateEvent(ctx, currentNode, failExuteMsg, v1.EventTypeWarning, gpuBenchmarkFinished)
		logrus.WithField("error", err).Fatal(failExuteMsg)
	}
	succedExuteMsg := "Succed to execute all_reduce_perf"
	generateEvent(ctx, currentNode, succedExuteMsg, v1.EventTypeNormal, gpuBenchmarkExecuted)
	logrus.WithField("output", succedExuteMsg).Info(gpuBenchmarkExecuted)

	perfOutput := string(output)
	logrus.WithField("output", perfOutput).Info(gpuBenchmarkExecuted)

	lines := strings.Split(perfOutput, "\n")
	var avgBandwidth float64

	foundLine := false
	for _, line := range lines {
		if strings.Contains(line, "Avg bus bandwidth") {
			parts := strings.Fields(line)
			avgBandwidth, err = strconv.ParseFloat(parts[len(parts)-1], 64)
			if err != nil {
				noOutput := "No AVG bandwidth output, test in trouble"
				generateEvent(ctx, currentNode, noOutput, v1.EventTypeWarning, gpuBenchmarkFinished)
				logrus.WithField("error", err).Fatal(noOutput)
			}
			foundLine = true
			break
		}
	}
	if !foundLine {
		logrus.Fatal("No AVG bandwidth output, test in trouble")
	}

	limitValue, err := strconv.ParseFloat(fmt.Sprintf("%f", *limit), 64)
	if err != nil {
		logrus.WithField("error", err).Fatal("Failed to parse limit value")
	}
	limitStr := strconv.FormatFloat(*limit, 'f', -1, 64) // Convert *limit to string

	if avgBandwidth < limitValue {
		succeed := 0
		logrus.WithField("avg_bandwidth", avgBandwidth).Info(fmt.Sprintf("Avg bus bandwidth: %f", avgBandwidth))
		messageReason := fmt.Sprintf(
			"The GPU benchmark ended with an unsatisfactory result for the NCCL test all_reduce_perf: Avg bus bandwidth=%f, min=%s",
			avgBandwidth,
			limitStr, // Use the converted limitStr
		)
		if *drainSlurmNode == true {
			cmd := exec.Command("scontrol", "update", "NodeName="+currentNode, "State=drain", "Reason="+messageReason)
			_, err := cmd.Output()
			if err != nil {
				failedDrainNodeMsg := fmt.Sprintf("Failed to drain node %s", currentNode)
				generateEvent(ctx, currentNode, failedDrainNodeMsg, v1.EventTypeWarning, gpuBenchmarkFinished)
				logrus.WithField("error", err).Fatal(failedDrainNodeMsg)
			}
			logrus.WithField("node", currentNode).Info("Node drained with reason: ", messageReason)
		}
		sendMetrics(ctx, currentNode, avgBandwidth, limitValue, succeed)
		generateEvent(ctx, currentNode, messageReason, v1.EventTypeWarning, gpuBenchmarkFinished)
		logrus.Fatal(messageReason)
	} else {
		succeed := 1
		logrus.WithField("avg_bandwidth", avgBandwidth).Info(fmt.Sprintf(
			"Avg bus bandwidth > %s: %f",
			limitStr, // Use the converted limitStr
			avgBandwidth))
		benchmarkFinishedMsg := fmt.Sprintf("GPU benchmark finished with Avg bus bandwidth=%f", avgBandwidth)
		logrus.WithField("avg_bandwidth", avgBandwidth).Info(benchmarkFinishedMsg)
		sendMetrics(ctx, currentNode, avgBandwidth, limitValue, succeed)
		generateEvent(ctx, currentNode, benchmarkFinishedMsg, v1.EventTypeNormal, gpuBenchmarkFinished)
	}

}
func initClientset(K8SServiceHost, K8SServicePort *string) (*kubernetes.Clientset, error) {
	var config *rest.Config

	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".kube", "config")); os.IsNotExist(err) {
		os.Setenv("KUBERNETES_SERVICE_HOST", *K8SServiceHost)
		os.Setenv("KUBERNETES_SERVICE_PORT", *K8SServicePort)
		// In-cluster config
		config, err = rest.InClusterConfig()
		if err != nil {
			logrus.WithField("error", err).Fatal("Failed to get in-cluster config")
		}
	} else {
		// Out-of-cluster config
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			logrus.WithField("error", err).Fatal("Failed to get out-of-cluster config")
		}
	}

	return kubernetes.NewForConfig(config)
}

func getMeter(ctx context.Context) metric.Meter {
	onceMeter.Do(func() {
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

		meter = otel.Meter(nameNCCL)
		shutdownMeterProvider, err := initMeterProvider(ctx, res, conn)
		if err != nil {
			log.Fatal(err)
		}

		defer func() {
			if err := shutdownMeterProvider(ctx); err != nil {
				log.Fatalf("failed to shutdown MeterProvider: %s", err)
			}
		}()
	})
	return meter
}

func sendMetrics(ctx context.Context, slurmNode string, avgBandwidth, limitValue float64, succeed int) {
	if *pushMetricsGrpc == false {
		meter := getMeter(ctx)
		commonAttrs := []attribute.KeyValue{
			attribute.String("namespace", *namespace),
			attribute.String("slurm_node", slurmNode),
		}
		avgBandwidthGauge, err := meter.Float64Gauge("slurm_jobs_avg_bandwidth", metric.WithDescription("Avg bus bandwidth"))
		if err != nil {
			logrus.WithField("error", err).Error("Failed to create job metric")
		}
		limitValueGauge, err := meter.Float64Gauge("slurm_jobs_limit_value", metric.WithDescription("Limit value"))
		if err != nil {
			logrus.WithField("error", err).Error("Failed to create job metric")
		}
		succeedGauge, err := meter.Int64Gauge("slurm_jobs_succeed", metric.WithDescription("Succeed jobs. 0 - failed, 1 - succeed"))
		if err != nil {
			logrus.WithField("error", err).Error("Failed to create job metric")
		}
		avgBandwidthGauge.Record(ctx, avgBandwidth, metric.WithAttributes(commonAttrs...))
		logrus.WithField("avg_bandwidth", avgBandwidth).Info("Metrics sent")

		limitValueGauge.Record(ctx, limitValue, metric.WithAttributes(commonAttrs...))
		logrus.WithField("limit_value", limitValue).Info("Metrics sent")

		succeedGauge.Record(ctx, int64(succeed), metric.WithAttributes(commonAttrs...))
		logrus.WithField("succeed", succeed).Info("Metrics sent")
	}
}

func generateEvent(ctx context.Context, currentNode, message, eventType, reason string) {
	if *pushEvents == true {
		onceClientset.Do(func() {
			clientset, err = initClientset(k8sServiceHost, k8sServicePort)
			if err != nil {
				logrus.WithField("error", err).Fatal("Failed to create clientset")
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
			logrus.WithField("error", err).Error("Failed to create event")
		}

		logrus.WithField("event", event).Info("Event created")
	}
}
