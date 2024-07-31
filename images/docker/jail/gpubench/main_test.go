package main

import (
	"context"
	"flag"
	"os"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestMainFunction(t *testing.T) {
	// Set command-line arguments
	os.Args = []string{"cmd", "--slurm_name=workerTest", "--message=This is a test event", "--type=Normal"}

	// Parse command-line arguments
	slurmNodeName := flag.String("slurm_name", "", "The reason for the event")
	message := flag.String("message", "", "The message for the event")
	eventType := flag.String("type", "", "The type of the event")
	flag.Parse()

	// Check command-line arguments
	if *slurmNodeName != "workerTest" || *message != "This is a test event" || *eventType != "Normal" {
		t.Fatalf("Command-line arguments are incorrect")
	}

	// Create fake client
	clientset := fake.NewSimpleClientset()

	namespace := "test"
	event := &v1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "slurm-node-",
			Namespace:    namespace,
		},
		Message:        *message,
		Reason:         *slurmNodeName,
		Type:           *eventType,
		LastTimestamp:  metav1.Now(),
		Source:         v1.EventSource{Component: "nccl-benchmark"},
		InvolvedObject: v1.ObjectReference{Kind: "Pod", Namespace: namespace, Name: *slurmNodeName},
	}

	ctx := context.TODO()
	opts := metav1.CreateOptions{}

	// Create event
	_, err := clientset.CoreV1().Events(namespace).Create(ctx, event, opts)
	if err != nil {
		t.Fatalf("Failed to create event: %v", err)
	}

	// List events
	events, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list events: %v", err)
	}

	// Check if event was created
	if len(events.Items) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events.Items))
	}

	// Check event properties
	if events.Items[0].Message != *message || events.Items[0].Reason != *slurmNodeName || events.Items[0].Type != *eventType {
		t.Errorf("Event properties are incorrect")
	}
}
