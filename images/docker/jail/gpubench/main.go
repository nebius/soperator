package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	gpuBenchmarkFinished = "GPUBenchmarkFinished"
	gpuBenchmarkExecuted = "GPUBenchmarkExecuted"
)

var (
	err error

	StepFactor      = flag.Int("step_factor", 2, "multiplication factor between sizes")
	Limit           = flag.Float64("limit", 420, "limit")
	MinBytes        = flag.String("min_bytes", "512M", "minimum size to start with")
	MaxBytes        = flag.String("max_bytes", "8G", "maximum size to end at")
	UseInfiniband   = flag.String("use_infiniband", "", "use infiniband for NCCL")
	Namespace       = flag.String("Namespace", "default", "kubernetes Namespace")
	K8SServiceHost  = flag.String("service_host", "kubernetes.default.svc", "kubernetes kube apiserver host")
	K8SServicePort  = flag.String("service_port", "443", "kubernetes kube apiserver port")
	DrainState      = flag.Bool("drain_state", false, "drain slurm node")
	PushEvents      = flag.Bool("push_events", false, "push events to kubernetes")
	PushMetricsHttp = flag.Bool("push_metrics_http", false, "push metrics to opentelemetry")
)

func init() {
	logrus.SetFormatter(&logrus.JSONFormatter{})
}

func main() {
	flag.Parse()

	clientset, err := initClientset(K8SServiceHost, K8SServicePort)
	if err != nil {
		logrus.WithField("error", err).Fatal("Failed to create clientset")
	}

	currentNode, err := os.Hostname()
	if err != nil {
		logrus.WithField("error", err).Fatal("Failed to get hostname")
	}

	if *UseInfiniband == "true" {
		os.Setenv("NCCL_P2P_DISABLE", "1")
		os.Setenv("NCCL_SHM_DISABLE", "1")
		os.Setenv("NCCL_ALGO", "Ring")
	}

	gpuCount := os.Getenv("SLURM_GPUS")
	if gpuCount == "" {
		logrus.WithField("error", "SLURM_GPUS").Fatal("Empty")
	}

	StepFactorStr := strconv.Itoa(*StepFactor) // Convert StepFactor to string for exec.Command

	cmd := exec.Command(
		"/usr/bin/all_reduce_perf",
		"-b", *MinBytes,
		"-e", *MaxBytes,
		"-f", StepFactorStr, // Use the converted StepFactorStr
		"-g", gpuCount,
	)

	ctx := context.TODO()

	output, err := cmd.Output()
	if err != nil {
		failExuteMsg := "Failed to execute all_reduce_perf"
		generateEventWrapper(clientset, *Namespace, currentNode, failExuteMsg, v1.EventTypeWarning, gpuBenchmarkFinished, ctx)
		logrus.WithField("error", err).Fatal(failExuteMsg)
	}
	succedExuteMsg := "Succed to execute all_reduce_perf"
	generateEventWrapper(clientset, *Namespace, currentNode, succedExuteMsg, v1.EventTypeNormal, gpuBenchmarkExecuted, ctx)
	logrus.WithField("output", succedExuteMsg).Info(gpuBenchmarkExecuted)

	perfOutput := string(output)
	logrus.WithField("output", perfOutput).Info(gpuBenchmarkExecuted)

	lines := strings.Split(perfOutput, "\n")
	var avgBandwidth float64

	for _, line := range lines {
		if strings.Contains(line, "Avg bus bandwidth") {
			parts := strings.Fields(line)
			avgBandwidth, err = strconv.ParseFloat(parts[len(parts)-1], 64)
			if err != nil {
				noOutput := "No AVG bandwidth output, test in trouble"
				generateEventWrapper(clientset, *Namespace, currentNode, noOutput, v1.EventTypeWarning, gpuBenchmarkFinished, ctx)
				logrus.WithField("error", err).Fatal(noOutput)
			}
			break
		}
	}

	limitValue, err := strconv.ParseFloat(fmt.Sprintf("%f", *Limit), 64)
	if err != nil {
		logrus.WithField("error", err).Fatal("Failed to parse limit value")
	}
	limitStr := strconv.FormatFloat(*Limit, 'f', -1, 64) // Convert *limit to string

	if avgBandwidth < limitValue {
		logrus.WithField("avg_bandwidth", avgBandwidth).Info(fmt.Sprintf("Avg bus bandwidth: %f", avgBandwidth))
		messageReason := fmt.Sprintf(
			"The GPU benchmark ended with an unsatisfactory result for the NCCL test all_reduce_perf: Avg bus bandwidth=%f, min=%s",
			avgBandwidth,
			limitStr, // Use the converted limitStr
		)
		if *DrainState == true {
			cmd := exec.Command("scontrol", "update", "NodeName="+currentNode, "State=drain", "Reason="+messageReason)
			_, err := cmd.Output()
			if err != nil {
				failedDrainNodeMsg := fmt.Sprintf("Failed to drain node %s", currentNode)
				generateEventWrapper(clientset, *Namespace, currentNode, failedDrainNodeMsg, v1.EventTypeWarning, gpuBenchmarkFinished, ctx)
				logrus.WithField("error", err).Fatal(failedDrainNodeMsg)
			}
			logrus.WithField("node", currentNode).Info("Node drained with reason: ", messageReason)
		}
		generateEventWrapper(clientset, *Namespace, currentNode, messageReason, v1.EventTypeWarning, gpuBenchmarkFinished, ctx)
		os.Exit(1)
	} else {
		logrus.WithField("avg_bandwidth", avgBandwidth).Info(fmt.Sprintf(
			"Avg bus bandwidth > %s: %f",
			limitStr, // Use the converted limitStr
			avgBandwidth))
		benchmarkFinishedMsg := fmt.Sprintf("GPU benchmark finished with Avg bus bandwidth=%f", avgBandwidth)
		logrus.WithField("avg_bandwidth", avgBandwidth).Info(benchmarkFinishedMsg)
		generateEventWrapper(clientset, *Namespace, currentNode, benchmarkFinishedMsg, v1.EventTypeNormal, gpuBenchmarkFinished, ctx)
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

// GenerateEventWrapper is a wrapper function to generateEvent
// Events are useful when there is no monitoring system in the cluster, as they allow users to understand what is wrong with the system
func generateEventWrapper(clientset *kubernetes.Clientset, Namespace string, currentNode, message, eventType, reason string, ctx context.Context) {
	if *PushEvents == true {
		generateEvent(clientset, Namespace, currentNode, message, eventType, reason, ctx)
	}
}

func generateEvent(clientset *kubernetes.Clientset, Namespace string, currentNode, message, eventType, reason string, ctx context.Context) {
	event := &v1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "slurm-node-",
			Namespace:    Namespace,
		},
		Reason:         reason,
		Message:        message,
		Type:           eventType,
		LastTimestamp:  metav1.Now(),
		Source:         v1.EventSource{Component: "nccl-benchmark"},
		InvolvedObject: v1.ObjectReference{Kind: "Pod", Namespace: Namespace, Name: currentNode},
	}

	opts := metav1.CreateOptions{}

	event, err := clientset.CoreV1().Events(Namespace).Create(ctx, event, opts)
	if err != nil {
		logrus.WithField("error", err).Fatal("Failed to create event")
	}

	logrus.WithField("event", event).Info("Event created")
}
