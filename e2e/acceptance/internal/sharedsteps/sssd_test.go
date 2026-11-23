package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

func TestDefaultSSSDConfigured(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*kubeobjects.SlurmCluster, *kubeobjects.NodeSet)
		want   bool
	}{
		{name: "defaults", want: true},
		{name: "login disabled", change: func(c *kubeobjects.SlurmCluster, _ *kubeobjects.NodeSet) { c.Spec.SlurmNodes.Login.SSSD = nil }},
		{name: "controller external config", change: func(c *kubeobjects.SlurmCluster, _ *kubeobjects.NodeSet) {
			c.Spec.SlurmNodes.Controller.SSSDConfSecretRefName = "external"
		}},
		{name: "worker disabled", change: func(_ *kubeobjects.SlurmCluster, n *kubeobjects.NodeSet) { n.Spec.SSSD = nil }},
		{name: "worker external CA", change: func(_ *kubeobjects.SlurmCluster, n *kubeobjects.NodeSet) {
			n.Spec.SSSDLdapCAConfigMapRefName = "external"
		}},
		{name: "foreign worker", want: true, change: func(_ *kubeobjects.SlurmCluster, n *kubeobjects.NodeSet) {
			n.Spec.ClusterName, n.Spec.SSSD = "another-cluster", nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var cluster kubeobjects.SlurmCluster
			require.NoError(t, json.Unmarshal([]byte(`{"metadata":{"name":"soperator"},"spec":{"slurmNodes":{"login":{"sssd":{}},"controller":{"sssd":{}}}}}`), &cluster))
			var worker kubeobjects.NodeSet
			require.NoError(t, json.Unmarshal([]byte(`{"spec":{"clusterName":"soperator","replicas":1,"sssd":{}}}`), &worker))
			if test.change != nil {
				test.change(&cluster, &worker)
			}
			assert.Equal(t, test.want, defaultSSSDConfigured(cluster, []kubeobjects.NodeSet{worker}))
		})
	}
}

type sssdTestRuntime struct {
	rolloutPollRuntime
	kubectl                framework.ArgsScope
	jail, controller, work framework.CommandScope
}

func (r *sssdTestRuntime) Kubectl() framework.ArgsScope                       { return r.kubectl }
func (r *sssdTestRuntime) Jail() framework.CommandScope                       { return r.jail }
func (r *sssdTestRuntime) Controller() framework.CommandScope                 { return r.controller }
func (r *sssdTestRuntime) Worker(framework.WorkerInfo) framework.CommandScope { return r.work }
func (*sssdTestRuntime) Logf(string, ...any)                                  {}

func TestSSSDQueriesTheResponderInEveryExecutionEnvironment(t *testing.T) {
	const identity = "nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n"
	for _, test := range []struct {
		name, main, failView, mismatchView string
		invalidSource                      bool
		wantViews                          []string
	}{
		{name: "login", main: "sshd", wantViews: []string{"sssd:/", "sshd:/", "sshd:/mnt/jail"}},
		{name: "controller", main: "slurmctld", wantViews: []string{"sssd:/", "slurmctld:/"}},
		{name: "worker", main: "slurmd", wantViews: []string{"sssd:/", "slurmd:/", "slurmd:/mnt/jail"}},
		{name: "accounting", main: "accounting", wantViews: []string{"sssd:/", "accounting:/"}},
		{name: "dead responder", main: "slurmd", failView: "sssd:/"},
		{name: "broken main NSS client", main: "slurmd", failView: "slurmd:/"},
		{name: "broken jail NSS client", main: "slurmd", failView: "slurmd:/mnt/jail"},
		{name: "wrong jail identity", main: "slurmd", mismatchView: "slurmd:/mnt/jail"},
		{name: "root source identity", main: "slurmd", invalidSource: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var views []string
			runtime := &sssdTestRuntime{}
			runtime.kubectl = framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
				command := strings.Join(args[7:], " ")
				if strings.HasPrefix(command, "getent -s files ") {
					assert.Equal(t, "sssd", args[5], "the source identity must come from the SSSD sidecar")
					if test.invalidSource {
						return "nobody:x:0:0::/:/bin/sh\n", nil
					}
					return identity, nil
				}
				root := "/"
				if args[7] == "chroot" {
					root = args[8]
				}
				view := args[5] + ":" + root
				views = append(views, view)
				assert.Contains(t, command, "SSS_NSS_USE_MEMCACHE=NO")
				assert.Contains(t, command, "getent -s sss passwd nobody")
				assert.Contains(t, command, "timeout 10s")
				if view == test.failView {
					return "", errors.New("SSSD responder unavailable")
				}
				if view == test.mismatchView {
					return "nobody:x:1234:65534:nobody:/nonexistent:/usr/sbin/nologin\n", nil
				}
				return identity, nil
			})
			steps := NewSSSD(nil, runtime, nil, nil, nil)
			steps.targets = []sssdTarget{{"test-pod", test.main}}
			err := steps.checkSSSD(t.Context())
			if test.wantViews == nil {
				require.Error(t, err)
				assert.NotErrorIs(t, err, godog.ErrSkip)
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.wantViews, views)
			}
		})
	}
}

func TestSSSDUserIdentityRejectsInvalidResponses(t *testing.T) {
	for _, output := range []string{"", "nobody:x:65534", "root:x:0:0::/:/bin/sh", "nobody:x:0:0::/:/bin/sh", "nobody:x::65534::/:/bin/sh"} {
		_, err := sssdUserIdentity(output, "nobody")
		require.Error(t, err)
	}
}

func TestSSSDLocalUserResolutionRejectsUIDMismatch(t *testing.T) {
	for _, workerUID := range []string{"1001", "1002"} {
		runtime := &sssdTestRuntime{}
		runtime.jail = framework.NewCommandScope(func(context.Context, string) (string, error) {
			return "bob:x:1001:1001::/home/bob:/bin/bash\n", nil
		})
		runtime.controller = runtime.jail
		runtime.work = framework.NewCommandScope(func(context.Context, string) (string, error) {
			return "bob:x:" + workerUID + ":1001::/home/bob:/bin/bash\n", nil
		})
		steps := NewSSSD(nil, runtime, nil, nil, nil)
		steps.worker = framework.WorkerInfo{Name: "worker-0"}
		err := steps.checkUser(t.Context())
		if workerUID == "1001" {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, "UID:GID")
		}
	}
}

func TestSSSDJobRunsAsTheLocalUserOnTheSelectedWorker(t *testing.T) {
	var command string
	runtime := &sssdTestRuntime{jail: framework.NewCommandScope(func(_ context.Context, value string) (string, error) {
		command = value
		return "JOB_USER=bob\nJOB_HOST=worker-0\n", nil
	})}
	steps := NewSSSD(nil, runtime, framework.NewSlurmClient(runtime), nil, nil)
	steps.worker = framework.WorkerInfo{Name: "worker-0"}
	require.NoError(t, steps.runJob(t.Context()))
	assert.Contains(t, command, "'-iu' 'bob'")
	assert.Contains(t, command, "'--nodelist=worker-0'")
	assert.Contains(t, command, "SSS_NSS_USE_MEMCACHE=NO getent -s sss passwd nobody >/dev/null && ")
	for _, output := range []string{"JOB_USER=root\nJOB_HOST=worker-0\n", "JOB_USER=bob\nJOB_HOST=worker-1\n", ""} {
		require.Error(t, steps.checkWorkloadIdentity(output))
	}
}
