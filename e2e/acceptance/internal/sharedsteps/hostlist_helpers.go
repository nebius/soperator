package sharedsteps

import (
	"context"
	"strings"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

// hostlistExpander turns a Slurm hostlist value that contains `[` into the concrete list of hostnames.
// Production wires it to `scontrol show hostnames`;
// tests with plain comma-separated values can pass nil.
type hostlistExpander func(value string) ([]string, error)

// cachedHostlistExpander expands Slurm hostlists.
// It asks the controller only for compressed ones and remembers each expansion:
// a hostlist always expands to the same names, so the cache never goes stale.
type cachedHostlistExpander struct {
	runtime framework.Runtime
	cache   map[string][]string
}

func newCachedHostlistExpander(runtime framework.Runtime) *cachedHostlistExpander {
	return &cachedHostlistExpander{runtime: runtime, cache: make(map[string][]string)}
}

// bind returns the expander as a hostlistExpander for parsers that take one.
func (e *cachedHostlistExpander) bind(ctx context.Context) hostlistExpander {
	return func(value string) ([]string, error) {
		return e.expand(ctx, value)
	}
}

func (e *cachedHostlistExpander) expand(ctx context.Context, value string) ([]string, error) {
	if nodes, ok := plainHostlist(value); ok {
		return nodes, nil
	}
	if nodes, ok := e.cache[value]; ok {
		return nodes, nil
	}

	nodes, err := framework.NewSlurmClient(e.runtime).ExpandHostnames(ctx, value)
	if err != nil {
		return nil, err
	}
	e.cache[value] = nodes
	return nodes, nil
}

// plainHostlist splits a hostlist that needs no expansion, reporting false when it carries a "["
// range that only scontrol can resolve.
func plainHostlist(value string) ([]string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || value == "(null)" {
		return nil, true
	}
	if strings.Contains(value, "[") {
		return nil, false
	}
	return splitSlurmList(value), true
}
