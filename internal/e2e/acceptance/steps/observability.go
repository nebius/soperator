package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const observabilityNamespace = "monitoring-system"

type Observability struct {
	exec framework.Exec
}

func NewObservability(exec framework.Exec) *Observability {
	return &Observability{exec: exec}
}

func (s *Observability) Register(sc *godog.ScenarioContext) {
	sc.Step(`^the kube-state-metrics VMServiceScrape carries the soperator scrape endpoints$`, s.checkKubeStateMetricsScrape)
}

// The soperator-fluxcd chart passes the kube-state-metrics scrape config to the
// victoria-metrics-k8s-stack chart under the `vmScrape` values key. Keys the
// upstream chart does not read are silently dropped, and the generated
// VMServiceScrape falls back to the chart's built-in default - a single http
// endpoint (see SCHED-2105, where exactly that hid the maxScrapeSize override).
// The soperator-defined config has two endpoints, so their presence proves the
// values reached the rendered object.
func (s *Observability) checkKubeStateMetricsScrape(ctx context.Context) error {
	var scrapes vmServiceScrapeList
	if err := kubectlJSON(ctx, s.exec, &scrapes, "get", "vmservicescrapes", "-n", observabilityNamespace, "-o", "json"); err != nil {
		return fmt.Errorf("list VMServiceScrapes: %w", err)
	}
	if len(scrapes.Items) == 0 {
		return fmt.Errorf("no VMServiceScrapes found in namespace %s - is observability enabled on this cluster?", observabilityNamespace)
	}

	var ksm []vmServiceScrape
	for _, scrape := range scrapes.Items {
		if strings.Contains(scrape.Metadata.Name, "kube-state-metrics") {
			ksm = append(ksm, scrape)
		}
	}
	if len(ksm) != 1 {
		names := make([]string, 0, len(ksm))
		for _, scrape := range ksm {
			names = append(names, scrape.Metadata.Name)
		}
		return fmt.Errorf("expected exactly one kube-state-metrics VMServiceScrape in %s, got %d [%s]",
			observabilityNamespace, len(ksm), strings.Join(names, ", "))
	}

	endpoints := ksm[0].Spec.Endpoints
	if len(endpoints) != 2 {
		return fmt.Errorf("%s has %d endpoints, expected 2 (http + metrics): the soperator scrape config is being ignored by the vm-stack chart",
			ksm[0].Metadata.Name, len(endpoints))
	}
	var problems []string
	if endpoints[0].Port != "http" {
		problems = append(problems, fmt.Sprintf("endpoints[0].port=%q, expected \"http\"", endpoints[0].Port))
	}
	if endpoints[1].Port != "metrics" {
		problems = append(problems, fmt.Sprintf("endpoints[1].port=%q, expected \"metrics\"", endpoints[1].Port))
	}
	for i, endpoint := range endpoints {
		if !hasLabeldrop(endpoint) {
			problems = append(problems, fmt.Sprintf("endpoints[%d] lost the labeldrop relabel config", i))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s: %s", ksm[0].Metadata.Name, strings.Join(problems, "; "))
	}
	return nil
}

func hasLabeldrop(endpoint vmScrapeEndpoint) bool {
	for _, relabel := range endpoint.MetricRelabelConfigs {
		if relabel.Action == "labeldrop" {
			return true
		}
	}
	return false
}

type vmServiceScrapeList struct {
	Items []vmServiceScrape `json:"items"`
}

type vmServiceScrape struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Endpoints []vmScrapeEndpoint `json:"endpoints"`
	} `json:"spec"`
}

type vmScrapeEndpoint struct {
	Port                 string `json:"port"`
	MaxScrapeSize        string `json:"max_scrape_size"`
	MetricRelabelConfigs []struct {
		Action string `json:"action"`
	} `json:"metricRelabelConfigs"`
}
