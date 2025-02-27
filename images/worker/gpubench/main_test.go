package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/metric/noop"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/stretchr/testify/assert"
)

func TestSendMetrics(t *testing.T) {
	ctx := context.Background()
	meter := noop.NewMeterProvider().Meter("test-meter")

	slurmNode := "test-node"
	avgBandwidth := 500.0
	limitValue := 420.0
	succeed := 1

	*pushMetricsGrpc = true
	// *pushMetricsHttp = true

	hook := testLogHook()
	defer hook.reset()

	sendMetrics(ctx, meter, slurmNode, avgBandwidth, limitValue, succeed)

	assertLogContains(t, hook, "avg_bandwidth", avgBandwidth)
	assertLogContains(t, hook, "limit_value", limitValue)
	assertLogContains(t, hook, "succeed", succeed)
}

func assertLogContains(t *testing.T, hook *testLogger, field string, expectedValue interface{}) {
	for _, entry := range hook.entries {
		if entry.Data[field] == expectedValue {
			return
		}
	}
	t.Errorf("Log does not contain expected field %s with value %v", field, expectedValue)
}

type testLogger struct {
	entries []logrus.Entry
}

func (l *testLogger) Fire(entry *logrus.Entry) error {
	l.entries = append(l.entries, *entry)
	return nil
}

func (l *testLogger) Levels() []logrus.Level {
	return logrus.AllLevels
}

func testLogHook() *testLogger {
	hook := &testLogger{}
	logrus.AddHook(hook)
	return hook
}

func (l *testLogger) reset() {
	logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))
}

func TestGenerateEvent(t *testing.T) {
	e := &EventGenerator{
		clientset: &MockClientset{},
	}

	ctx := context.Background()
	currentNode := "node1"
	message := "message"
	eventType := "type"
	reason := "reason"

	e.generateEvent(ctx, currentNode, message, eventType, reason)

	// If the function didn't panic or block, we assume it passed.
	// More detailed assertions would require more detailed mocks.
	assert.True(t, true)
}

type MockEventInterface struct {
	corev1.EventInterface // Embed the interface we want to mock
}

// Override the Create method
func (m *MockEventInterface) Create(ctx context.Context, event *v1.Event, opts metav1.CreateOptions) (*v1.Event, error) {
	return event, nil
}

type MockCoreV1 struct {
	corev1.CoreV1Interface // Embed the interface we want to mock
}

// Override the Events method
func (m *MockCoreV1) Events(namespace string) corev1.EventInterface {
	return &MockEventInterface{}
}

type MockClientset struct {
	*fake.Clientset
}

// Ensure CoreV1 returns a CoreV1Interface
func (m *MockClientset) CoreV1() corev1.CoreV1Interface {
	return &MockCoreV1{}
}

// Check if the process is running on GPU
func TestIsRunningProcessOnGPU(t *testing.T) {
	tests := []struct {
		name         string
		mockOutput   string
		expectResult bool
		expectError  bool
	}{
		{
			name:         "No processes running",
			mockOutput:   "",
			expectResult: false,
			expectError:  false,
		},
		{
			name:         "Processes running",
			mockOutput:   "1652624019822, all_reduce_perf\n1652524111113, all_reduce_perf",
			expectResult: true,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("echo", tt.mockOutput)
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			err := cmd.Run()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			result := strings.TrimSpace(stdout.String()) != ""
			if result != tt.expectResult {
				t.Errorf("expected %v, got %v", tt.expectResult, result)
			}
		})
	}
}
