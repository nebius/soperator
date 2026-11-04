package framework

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlurmClientConfiguration(t *testing.T) {
	runtime := &slurmConfigurationRuntime{output: `Configuration data as of 2026-09-01T15:00:00
MpiDefault              = pmix
SlurmdTimeout           = 180 sec
PluginOption            = name=value
`}

	configuration, err := NewSlurmClient(runtime).Configuration(t.Context())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"MpiDefault":    "pmix",
		"PluginOption":  "name=value",
		"SlurmdTimeout": "180 sec",
	}, configuration)
}

func TestParseSlurmConfigurationRejectsInvalidOutput(t *testing.T) {
	for name, output := range map[string]string{
		"no settings":    "Configuration data as of today\n",
		"empty setting":  " = value\n",
		"duplicate name": "MpiDefault = pmix\nMpiDefault = none\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseSlurmConfiguration(output)
			assert.Error(t, err)
		})
	}
}

type slurmConfigurationRuntime struct {
	Runtime
	output string
}

func (r *slurmConfigurationRuntime) Controller() CommandScope {
	return NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if command != "scontrol show config" {
			return "", fmt.Errorf("unexpected controller command: %s", command)
		}
		return r.output, nil
	})
}

func TestSlurmClientSuspendExclusions(t *testing.T) {
	for _, tt := range []struct {
		name, value, hostlist, hosts string
		counted                      bool
		want                         map[string]bool
	}{
		{name: "host ranges", value: "worker-[0,3-5]", hostlist: "worker-[0,3-5]", hosts: "worker-0\nworker-3\nworker-4\nworker-5", want: map[string]bool{"worker-0": true, "worker-3": true, "worker-4": true, "worker-5": true}},
		{name: "null", value: "(null)"},
		{name: "empty", value: ""},
		{name: "counted only", value: "worker-[0-9]:2", counted: true},
		{name: "mixed", value: "worker-[0,3],other-[0-9]:2,last-0", hostlist: "worker-[0,3],last-0", hosts: "worker-0\nworker-3\nlast-0", counted: true, want: map[string]bool{"worker-0": true, "worker-3": true, "last-0": true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			replies := []slurmSuspensionReply{{command: "scontrol show config", output: "  SuspendExcNodes = " + tt.value + "\nSuspendExcParts = other", retry: true}}
			if tt.hostlist != "" {
				replies = append(replies, slurmSuspensionReply{
					command: "scontrol show hostnames " + ShellQuote(tt.hostlist), output: tt.hosts, retry: true,
				})
			}
			runtime := newSlurmSuspensionRuntime(t, replies...)
			got, err := NewSlurmClient(runtime).SuspendExclusions(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tt.counted, got.Counted)
			if len(tt.want) == 0 {
				assert.Empty(t, got.Names)
			} else {
				assert.Equal(t, tt.want, got.Names)
			}
		})
	}
}

func TestSlurmClientSuspendExclusionsClassifiesInvalidOutput(t *testing.T) {
	for name, config := range map[string]string{
		"missing field":    "SuspendExcParts = other",
		"duplicate field":  "SuspendExcNodes = a\nSuspendExcNodes = b",
		"unclosed bracket": "SuspendExcNodes = worker-[0",
		"invalid config":   " = invalid",
	} {
		t.Run(name, func(t *testing.T) {
			runtime := newSlurmSuspensionRuntime(t, slurmSuspensionReply{command: "scontrol show config", output: config, retry: true})
			_, err := NewSlurmClient(runtime).SuspendExclusions(t.Context())
			var invalid SlurmConfigurationError
			require.ErrorAs(t, err, &invalid)
		})
	}
	for _, hosts := range []string{"", "worker-0 extra", "worker-[0-2]"} {
		t.Run("hosts="+hosts, func(t *testing.T) {
			runtime := newSlurmSuspensionRuntime(t,
				slurmSuspensionReply{command: "scontrol show config", output: "SuspendExcNodes = worker-[0-2]", retry: true},
				slurmSuspensionReply{command: "scontrol show hostnames 'worker-[0-2]'", output: hosts, retry: true})
			_, err := NewSlurmClient(runtime).SuspendExclusions(t.Context())
			var invalid SlurmConfigurationError
			require.ErrorAs(t, err, &invalid)
			require.ErrorContains(t, err, "invalid node name")
		})
	}
}

func TestSlurmClientSuspendExclusionsPreservesCommandErrors(t *testing.T) {
	commandErr := errors.New("controller unavailable")
	for _, hostRead := range []bool{false, true} {
		t.Run(fmt.Sprintf("hostRead=%t", hostRead), func(t *testing.T) {
			replies := []slurmSuspensionReply{{command: "scontrol show config", output: "SuspendExcNodes = worker-0", retry: true}}
			if hostRead {
				replies = append(replies, slurmSuspensionReply{command: "scontrol show hostnames 'worker-0'", retry: true})
			}
			replies[len(replies)-1].err = commandErr
			runtime := newSlurmSuspensionRuntime(t, replies...)
			_, err := NewSlurmClient(runtime).SuspendExclusions(t.Context())
			assert.ErrorIs(t, err, commandErr)
			var invalid SlurmConfigurationError
			assert.NotErrorAs(t, err, &invalid)
		})
	}
}

func TestSlurmClientUpdatesSuspendExclusionsWithoutRetry(t *testing.T) {
	commandErr := errors.New("response lost")
	runtime := newSlurmSuspensionRuntime(t,
		slurmSuspensionReply{command: "scontrol update 'SuspendExcNodes+=worker-0,worker-3'"},
		slurmSuspensionReply{command: "scontrol update 'SuspendExcNodes-=worker-0,worker-3'", err: commandErr},
	)
	client := NewSlurmClient(runtime)
	require.NoError(t, client.AddSuspendExclusions(t.Context(), []string{"worker-0", "worker-3"}))
	assert.ErrorIs(t, client.RemoveSuspendExclusions(t.Context(), []string{"worker-0", "worker-3"}), commandErr)
}

func TestSlurmClientSuspendUpdatesRejectNonliteralNames(t *testing.T) {
	client := NewSlurmClient(newSlurmSuspensionRuntime(t))
	for _, names := range [][]string{nil, {"worker-[0-3]"}, {"worker-3;echo bad"}, {"worker-0,worker-3"}, {""}} {
		require.Error(t, client.AddSuspendExclusions(t.Context(), names))
		require.Error(t, client.RemoveSuspendExclusions(t.Context(), names))
	}
}

func TestSplitSuspendExclusions(t *testing.T) {
	terms, err := splitSuspendExclusions("worker-[0,3-5],other-[0-9]:2,last-0")
	require.NoError(t, err)
	assert.Equal(t, []string{"worker-[0,3-5]", "other-[0-9]:2", "last-0"}, terms)
	terms, err = splitSuspendExclusions("")
	require.NoError(t, err)
	assert.Empty(t, terms)
	for _, bad := range []string{"worker-[0", "worker-0]", "worker-[[0]]", "worker-0,", ",worker-0", "worker-0,,other"} {
		_, err := splitSuspendExclusions(bad)
		require.Error(t, err, bad)
	}
}

type slurmSuspensionReply struct {
	command string
	output  string
	err     error
	retry   bool
}

type slurmSuspensionRuntime struct {
	Runtime
	t       *testing.T
	replies []slurmSuspensionReply
}

func newSlurmSuspensionRuntime(t *testing.T, replies ...slurmSuspensionReply) *slurmSuspensionRuntime {
	t.Helper()
	r := &slurmSuspensionRuntime{t: t, replies: replies}
	t.Cleanup(func() { assert.Empty(t, r.replies, "unconsumed Slurm commands") })
	return r
}

func (r *slurmSuspensionRuntime) Controller() CommandScope {
	return slurmSuspensionScope{r: r}
}

type slurmSuspensionScope struct {
	CommandScope
	r *slurmSuspensionRuntime
}

func (s slurmSuspensionScope) Run(_ context.Context, command string) (string, error) {
	return s.reply(command, false)
}

func (s slurmSuspensionScope) RunWithDefaultRetry(_ context.Context, command string) (string, error) {
	return s.reply(command, true)
}

func (s slurmSuspensionScope) reply(command string, retry bool) (string, error) {
	s.r.t.Helper()
	require.NotEmpty(s.r.t, s.r.replies, "unexpected Slurm command: %s", command)
	reply := s.r.replies[0]
	s.r.replies = s.r.replies[1:]
	require.Equal(s.r.t, reply.command, command)
	require.Equal(s.r.t, reply.retry, retry, "retry semantics for %s", command)
	return reply.output, reply.err
}
