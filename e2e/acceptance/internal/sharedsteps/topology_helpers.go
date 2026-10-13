package sharedsteps

import (
	"context"
	"fmt"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func expandSlurmHostlist(
	ctx context.Context,
	runtime framework.Runtime,
	value string,
	cache map[string][]string,
) ([]string, error) {
	if nodes, ok := plainHostlist(value); ok {
		return nodes, nil
	}
	if nodes, ok := cache[value]; ok {
		return nodes, nil
	}

	out, err := runtime.Controller().RunWithDefaultRetry(ctx,
		fmt.Sprintf("scontrol show hostnames %s", framework.ShellQuote(value)))
	if err != nil {
		return nil, fmt.Errorf("expand hostlist %q: %w", value, err)
	}
	nodes := framework.ParseSlurmNodeNames(out)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("hostlist %q expanded to nothing", value)
	}
	cache[value] = nodes
	return nodes, nil
}
