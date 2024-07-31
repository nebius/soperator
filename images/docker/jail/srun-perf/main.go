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

var (
	err error

	minBytes       = flag.String("b", "", "min_bytes")
	maxBytes       = flag.String("e", "", "max_bytes")
	stepFactor     = flag.String("f", "", "step_factor")
	limit          = flag.String("l", "", "limit")
	drainState     = flag.String("d", "", "drain_state")
	useInfiniband  = flag.String("u", "", "use_infiniband")
	namespace      = flag.String("namespace", "", "namespace")
	k8sServiceHost = flag.String("service_host", "", "kubernetes_service_host")
	k8sServicePort = flag.String("service_port", "", "kubernetes_service_port")
)

func init() {
	logrus.SetFormatter(&logrus.JSONFormatter{})
}

func main() {
	flag.Parse()
	flags := []*string{minBytes, maxBytes, stepFactor, limit, drainState, useInfiniband, namespace}
	for _, flag := range flags {
		if *flag == "" {
			logrus.WithField("error", "flag is empty").Fatal(
				fmt.Sprintf("All flags must be provided. Flag: %s is empty", *flag),
			)
		}
	}

	clientset, err := initClientset(k8sServiceHost, k8sServicePort)
	if err != nil {
		logrus.WithField("error", err).Fatal("Failed to create clientset")
	}

	currentNode, err := os.Hostname()
	if err != nil {
		logrus.WithField("error", err).Fatal("Failed to get hostname")
	}

	if err != nil {
		logrus.WithField("error", err).Fatal("Failed to create clientset")
	}

	if *useInfiniband == "true" {
		os.Setenv("NCCL_P2P_DISABLE", "1")
		os.Setenv("NCCL_SHM_DISABLE", "1")
		os.Setenv("NCCL_ALGO", "Ring")
	}

	gpuCount := os.Getenv("SLURM_GPUS")
	if gpuCount == "" {
		logrus.WithField("error", "SLURM_GPUS is empty").Fatal("SLURM_GPUS is empty")
	}

	cmd := exec.Command(
		"/usr/bin/all_reduce_perf",
		"-b", *minBytes,
		"-e", *maxBytes,
		"-f", *stepFactor,
		"-g", gpuCount,
	)

	ctx := context.TODO()

	output, err := cmd.Output()
	if err != nil {
		generateEvent(clientset, *namespace, currentNode, "Failed to execute all_reduce_perf", "Failed", "AllReducePerd", ctx)
		logrus.WithField("error", err).Fatal("Failed to execute all_reduce_perf")
	}
	generateEvent(clientset, *namespace, currentNode, "Succed to execute all_reduce_perf", "Normal", "AllReducePerd", ctx)

	perfOutput := string(output)
	logrus.WithField("output", perfOutput).Info("all_reduce_perf output")

	lines := strings.Split(perfOutput, "\n")
	var avgBandwidth float64

	for _, line := range lines {
		if strings.Contains(line, "Avg bus bandwidth") {
			parts := strings.Fields(line)
			avgBandwidth, err = strconv.ParseFloat(parts[len(parts)-1], 64)
			if err != nil {
				logrus.WithField("error", err).Fatal("No AVG bandwidth output, test in trouble")
			}
			break
		}
	}

	limitValue, err := strconv.ParseFloat(*limit, 64)
	if err != nil {
		logrus.WithField("error", err).Fatal("Failed to parse limit value")
	}

	if avgBandwidth < limitValue {
		logrus.WithField("avg_bandwidth", avgBandwidth).Info(fmt.Sprintf("Avg bus bandwidth: %f", avgBandwidth))
		messageReason := fmt.Sprintf(
			" GPU benchmark ended with unsatisfactoryresult: NCCL test all_reduce_perf: Avg bus bandwidth=%f, min=%s",
			avgBandwidth,
			*limit,
		)
		if *drainState == "true" {
			cmd := exec.Command("scontrol", "update", "NodeName="+currentNode, "State=drain", "Reason="+messageReason)
			_, err := cmd.Output()
			if err != nil {
				logrus.WithField("error", err).Fatal("Failed to drain node")
			}
			logrus.WithField("node", currentNode).Info("Node drained with reason: ", messageReason)
		}
		generateEvent(clientset, *namespace, currentNode, messageReason, "Failed", "GPUBenchmarkFinished", ctx)
		os.Exit(1)
	} else {
		logrus.WithField("avg_bandwidth", avgBandwidth).Info(fmt.Sprintf("Avg bus bandwidth > %s: %f", *limit, avgBandwidth))
		logrus.WithField("avg_bandwidth", avgBandwidth).Info("Performance test completed")
		generateEvent(clientset, *namespace, currentNode, "GPU benchmark succeed", "Normal", "GPUBenchmarkFinished", ctx)
	}

}
func initClientset(k8sServiceHost, k8sServicePort *string) (*kubernetes.Clientset, error) {
	var config *rest.Config

	if k8sServiceHost == nil {
		k8sServiceHost = new(string)
		*k8sServiceHost = "kubernetes.default.svc"
	}
	if k8sServicePort == nil {
		k8sServicePort = new(string)
		*k8sServicePort = "443"
	}

	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".kube", "config")); os.IsNotExist(err) {
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

func generateEvent(clientset *kubernetes.Clientset, namespace string, currentNode, message, eventType, reason string, ctx context.Context) {
	event := &v1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "slurm-node-",
			Namespace:    namespace,
		},
		Reason:         reason,
		Message:        message,
		Type:           eventType,
		LastTimestamp:  metav1.Now(),
		Source:         v1.EventSource{Component: "nccl-benchmark"},
		InvolvedObject: v1.ObjectReference{Kind: "Pod", Namespace: namespace, Name: currentNode},
	}

	opts := metav1.CreateOptions{}

	event, err := clientset.CoreV1().Events(namespace).Create(ctx, event, opts)
	if err != nil {
		logrus.WithField("error", err).Fatal("Failed to create event")
	}

	logrus.WithField("event", event).Info("Event created")
}
