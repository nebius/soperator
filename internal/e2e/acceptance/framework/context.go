//go:build acceptance

package framework

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
)

type WorkerRef struct {
	Name string
}

type Suite struct {
	exec    *Executor
	workers []WorkerRef
	report  *SummaryReporter
}

func LoadSuite(ctx context.Context) (*Suite, error) {
	suite := &Suite{
		report: NewSummaryReporter(),
	}
	suite.exec = NewExecutor(10*time.Minute, suite.report)

	if err := suite.discoverCluster(ctx); err != nil {
		return suite, err
	}

	return suite, nil
}

func (s *Suite) WorkerCount() int {
	return len(s.workers)
}

func (s *Suite) AnyWorker() (WorkerRef, error) {
	if len(s.workers) == 0 {
		return WorkerRef{}, fmt.Errorf("no workers discovered")
	}

	return s.workers[rand.Intn(len(s.workers))], nil
}

func (s *Suite) discoverCluster(ctx context.Context) error {
	s.Logf("discovering acceptance cluster")

	if _, err := s.exec.Run(ctx, "kubectl", "get", "pods", "-n", soperatorNamespace); err != nil {
		return err
	}
	if _, err := s.exec.Run(ctx, "kubectl", "get", "pod", "-n", soperatorNamespace, "login-0"); err != nil {
		return err
	}
	if _, err := s.exec.Run(ctx, "kubectl", "get", "pod", "-n", soperatorNamespace, "controller-0"); err != nil {
		return err
	}

	workerOutput, err := s.exec.ExecController(ctx, `sinfo -hN -p main -o '%N'`)
	if err != nil {
		return fmt.Errorf("discover worker nodes: %w", err)
	}

	var workers []WorkerRef
	for _, line := range strings.Split(workerOutput, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		workers = append(workers, WorkerRef{Name: name})
	}
	if len(workers) == 0 {
		return fmt.Errorf("no worker nodes discovered")
	}

	s.workers = workers
	s.Logf("discovered workers: %s", workerNames(workers))

	return nil
}

func (s *Suite) Logf(format string, args ...any) {
	s.exec.Logf(format, args...)
}

func (s *Suite) Step(ctx SpecContext, name string, body func(SpecContext)) {
	s.runStep(ctx, "", name, body)
}

func (s *Suite) Given(ctx SpecContext, name string, body func(SpecContext)) {
	s.runStep(ctx, "Given", name, body)
}

func (s *Suite) When(ctx SpecContext, name string, body func(SpecContext)) {
	s.runStep(ctx, "When", name, body)
}

func (s *Suite) Then(ctx SpecContext, name string, body func(SpecContext)) {
	s.runStep(ctx, "Then", name, body)
}

func (s *Suite) And(ctx SpecContext, name string, body func(SpecContext)) {
	s.runStep(ctx, "And", name, body)
}

func (s *Suite) runStep(ctx SpecContext, keyword, name string, body func(SpecContext)) {
	byText := strings.TrimSpace(strings.Join([]string{keyword, name}, " "))
	By(byText)

	report := CurrentSpecReport()
	token := s.report.StartStep(report, keyword, name)

	defer func() {
		if recovered := recover(); recovered != nil {
			s.report.FinishStep(token, StepStatusFailed)
			panic(recovered)
		}

		s.report.FinishStep(token, StepStatusPassed)
	}()

	body(ctx)
}

func (s *Suite) WriteSummary(report types.Report) error {
	return s.report.WriteSummary(report)
}

func (s *Suite) Run(ctx context.Context, name string, args ...string) (string, error) {
	return s.exec.Run(ctx, name, args...)
}

func (s *Suite) ExecController(ctx context.Context, command string) (string, error) {
	return s.exec.ExecController(ctx, command)
}

func (s *Suite) ExecJail(ctx context.Context, command string) (string, error) {
	return s.exec.ExecJail(ctx, command)
}

func (s *Suite) ExecJailWithRetry(ctx context.Context, command string, attempts int, delay time.Duration) (string, error) {
	return s.exec.ExecJailWithRetry(ctx, command, attempts, delay)
}

func workerNames(workers []WorkerRef) string {
	names := make([]string, 0, len(workers))
	for _, worker := range workers {
		names = append(names, worker.Name)
	}

	return strings.Join(names, ", ")
}
