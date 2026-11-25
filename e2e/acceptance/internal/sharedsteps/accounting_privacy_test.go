package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func privacyTestFixture() *accountingPrivacyState {
	p := newAccountingPrivacyState("e2epunit")
	for i, user := range p.Users[:3] {
		id := fmt.Sprint(i + 1)
		workdir := user.Home + "/work"
		marker := p.Prefix + "_" + user.Role
		name := p.Prefix + "-" + user.Role
		p.Jobs = append(p.Jobs, accountingPrivacyJob{
			Job: framework.SbatchJob{ID: id, JobName: name}, Marker: marker,
			Record: accountingPrivacyJobRecord{ID: id, User: user.Name, Account: user.Account, Name: name,
				State: "COMPLETED", AssocID: fmt.Sprint(i + 10), WorkDir: workdir,
				SubmitLine: "sbatch --chdir=" + workdir + " --wrap='printf " + marker + "'"},
		})
	}
	return p
}

func privacyJobRow(record accountingPrivacyJobRecord) string {
	return strings.Join([]string{record.ID, record.User, record.Account, record.Name, record.State,
		record.AssocID, record.WorkDir, record.SubmitLine}, "|") + "\n"
}

func TestAccountingPrivacyRequiresOwnRecordsAndExactSensitiveFields(t *testing.T) {
	job := privacyTestFixture().Jobs[0].Record
	require.NoError(t, validateAccountingPrivacyJobs(privacyJobRow(job), []accountingPrivacyJobRecord{job}))
	for _, tc := range []struct {
		name   string
		output string
	}{
		{"empty own result", ""},
		{"duplicate job", privacyJobRow(job) + privacyJobRow(job)},
		{"malformed row", "1|user|account\n"},
		{"different job ID", strings.Replace(privacyJobRow(job), "1|", "99|", 1)},
		{"foreign owner", strings.Replace(privacyJobRow(job), job.User, "foreign", 1)},
		{"different working directory", strings.Replace(privacyJobRow(job), job.WorkDir, "/foreign", 1)},
		{"different submit command", strings.TrimSuffix(privacyJobRow(job), "\n") + " --foreign\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, validateAccountingPrivacyJobs(tc.output, []accountingPrivacyJobRecord{job}))
		})
	}
	require.NoError(t, validateAccountingPrivacyJobs("", nil))
	require.Error(t, validateAccountingPrivacyJobs(privacyJobRow(job), nil))
}

func TestAccountingPrivacyBaselineRejectsUnassociatedAndIncompleteJobs(t *testing.T) {
	job := privacyTestFixture().Jobs[0]
	jobs, err := captureAccountingPrivacyJobs(privacyJobRow(job.Record), []accountingPrivacyJob{job})
	require.NoError(t, err)
	require.Equal(t, job.Record, jobs[0].Record)
	for _, change := range []func(*accountingPrivacyJobRecord){
		func(r *accountingPrivacyJobRecord) { r.AssocID = "0" },
		func(r *accountingPrivacyJobRecord) { r.AssocID = "unknown" },
		func(r *accountingPrivacyJobRecord) { r.State = "RUNNING" },
		func(r *accountingPrivacyJobRecord) { r.User = "foreign" },
		func(r *accountingPrivacyJobRecord) { r.Account = "other" },
		func(r *accountingPrivacyJobRecord) { r.SubmitLine = "sbatch --chdir=" + r.WorkDir },
		func(r *accountingPrivacyJobRecord) { r.SubmitLine = "sbatch " + job.Marker },
	} {
		record := job.Record
		change(&record)
		_, err := captureAccountingPrivacyJobs(privacyJobRow(record), []accountingPrivacyJob{job})
		require.Error(t, err, "%+v", record)
	}
	_, err = captureAccountingPrivacyJobs("", []accountingPrivacyJob{job})
	require.Error(t, err)
}

func TestAccountingPrivacyRoleMatrix(t *testing.T) {
	p := privacyTestFixture()
	for _, tc := range []struct {
		name         string
		index        int
		privileged   bool
		jobs         []string
		users        []string
		associations int
	}{
		{name: "alice", index: 0, jobs: []string{"1"}, users: []string{p.Users[0].Name}, associations: 1},
		{name: "bob", index: 1, jobs: []string{"2"}, users: []string{p.Users[1].Name}, associations: 1},
		{name: "carol", index: 2, jobs: []string{"3"}, users: []string{p.Users[2].Name}, associations: 1},
		{name: "coordinator", index: 3, jobs: []string{"1", "2"}, users: []string{p.Users[3].Name}, associations: 4},
		{name: "Operator or Admin", index: 4, privileged: true, jobs: []string{"1", "2", "3"}, users: p.userNames()[:5], associations: 5},
		{name: "no DB record", index: 5},
		{name: "root", index: -1, privileged: true, jobs: []string{"1", "2", "3"}, users: p.userNames()[:5], associations: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := accountingPrivacyUser{Name: "root"}
			if tc.index >= 0 {
				user = p.Users[tc.index]
			}
			jobs, users, associations := p.permissions(user.Name, user.Account, user.Role == "coord", tc.privileged)
			var ids []string
			for _, record := range jobs {
				ids = append(ids, record.ID)
			}
			assert.Equal(t, tc.jobs, ids)
			assert.Equal(t, tc.users, users)
			require.Len(t, associations, tc.associations)
			if user.Role == "coord" {
				for _, association := range associations {
					assert.True(t, strings.HasSuffix(association, "|"+p.Accounts[0]), association)
				}
			}
		})
	}
}

func TestAccountingPrivacyRejectsLeakedUsersAndAssociations(t *testing.T) {
	require.NoError(t, validateAccountingPrivacyRows("alice|a|\n", 2, []string{"alice|a"}))
	for _, output := range []string{"", "alice|a|\nbob|a|\n", "alice|b|\n", "alice|a|extra|\n", "alice|a|\nalice|a|\n"} {
		require.Error(t, validateAccountingPrivacyRows(output, 2, []string{"alice|a"}), output)
	}
}

func TestAccountingPrivacyNSSIdentityValidation(t *testing.T) {
	identities, err := parseAccountingPrivacyIdentities("alice:x:1001:1002::/home/alice:/bin/bash\n")
	require.NoError(t, err)
	assert.Equal(t, accountingPrivacyIdentity{UID: "1001", GID: "1002"}, identities["alice"])
	for _, output := range []string{"", "alice", "alice:x:0:0::/root:/bin/bash", "alice:x:1001:unknown::/home/alice:/bin/bash",
		"alice:x:1001:1002::/home/alice:/bin/bash\nalice:x:1002:1002::/home/alice:/bin/bash"} {
		_, err := parseAccountingPrivacyIdentities(output)
		require.Error(t, err, output)
	}
}

func TestAccountingPrivacyConfigAndOriginalFieldRestoration(t *testing.T) {
	flags, err := accountingPrivacyConfigFlags("PrivateData = users,jobs,usage\n")
	require.NoError(t, err)
	assert.Equal(t, "jobs,usage,users", flags)
	for _, output := range []string{"PrivateData = jobs\nPrivateData = users", "unexpected output"} {
		_, err = accountingPrivacyConfigFlags(output)
		require.Error(t, err)
	}
	flags, err = accountingPrivacyConfigFlags("")
	require.NoError(t, err)
	assert.Empty(t, flags)
	for _, value := range []*string{nil, stringPointer(""), stringPointer("none")} {
		assert.Empty(t, accountingPrivacyFlags(value))
	}
	for _, tc := range []struct {
		value *string
		want  string
	}{{nil, "null"}, {stringPointer(""), `""`}, {stringPointer("jobs,usage,users"), `"jobs,usage,users"`}} {
		patch, err := accountingPrivacyPatch(tc.value)
		require.NoError(t, err)
		assert.JSONEq(t, `{"spec":{"slurmNodes":{"accounting":{"slurmdbdConfig":{"privateData":`+tc.want+`}}}}}`, string(patch))
	}
}

func privacyReadyWorkload() (appsv1.Deployment, corev1.Pod) {
	one := int32(1)
	deployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-accounting", Generation: 2},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
		Status:     appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1},
	}
	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "custom-accounting-new", UID: "new-uid"},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
	return deployment, pod
}

func TestAccountingPrivacyWaitsForNewReadyPodAndObservedGeneration(t *testing.T) {
	deployment, pod := privacyReadyWorkload()
	_, err := accountingPrivacyReadyPod(deployment, []corev1.Pod{pod}, "old-uid")
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		change func(*appsv1.Deployment, *corev1.Pod)
	}{
		{"old pod", func(_ *appsv1.Deployment, p *corev1.Pod) { p.UID = "old-uid" }},
		{"missing UID", func(_ *appsv1.Deployment, p *corev1.Pod) { p.UID = "" }},
		{"terminating", func(_ *appsv1.Deployment, p *corev1.Pod) { now := metav1.Now(); p.DeletionTimestamp = &now }},
		{"not Ready", func(_ *appsv1.Deployment, p *corev1.Pod) { p.Status.Conditions = nil }},
		{"not Running", func(_ *appsv1.Deployment, p *corev1.Pod) { p.Status.Phase = corev1.PodPending }},
		{"stale generation", func(d *appsv1.Deployment, _ *corev1.Pod) { d.Status.ObservedGeneration = 1 }},
		{"not updated", func(d *appsv1.Deployment, _ *corev1.Pod) { d.Status.UpdatedReplicas = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dep, pod := privacyReadyWorkload()
			tc.change(&dep, &pod)
			_, err := accountingPrivacyReadyPod(dep, []corev1.Pod{pod}, "old-uid")
			require.Error(t, err)
		})
	}
}

type privacyCleanupRuntime struct {
	framework.Runtime
	privateData              *string
	ready                    bool
	failUser                 string
	commands                 []string
	patches                  []string
	waitAttempts             int
	clientsPendingAfterPatch int
	pendingClients           int
	prematureCommands        int
	identities               string
	workerSnapshots          []corev1.PodList
	workerReads              int
	workerCommands           []string
	workerExecFailures       int
	workerSSHCalls           int
	kubectlCommands          []string
	slurmNodeNames           string
	slurmNodes               []string
	slurmReads               int
}

func (r *privacyCleanupRuntime) Logf(string, ...any) {}

func (r *privacyCleanupRuntime) WaitFor(ctx context.Context, _ string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	var lastErr error
	for range max(1, r.waitAttempts) {
		if err := ctx.Err(); err != nil {
			return err
		}
		done, err := condition(ctx)
		if err == nil && done {
			return nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return lastErr
	}
	return errors.New("test condition is not ready")
}

func (r *privacyCleanupRuntime) Jail() framework.CommandScope {
	return framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		r.commands = append(r.commands, command)
		if r.pendingClients > 0 {
			r.prematureCommands++
			return "", errors.New("login pod is not ready")
		}
		if r.failUser != "" && strings.Contains(command, "deluser --remove-home '"+r.failUser+"'") {
			return "", errors.New("delete test user: injected error")
		}
		if strings.HasPrefix(command, "getent passwd ") {
			return r.identities, nil
		}
		return "", nil
	})
}

func (r *privacyCleanupRuntime) Controller() framework.CommandScope {
	return framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if command == "sinfo -hN -p main -o '%N'" {
			r.commands = append(r.commands, command)
			if r.slurmNodeNames != "" {
				return r.slurmNodeNames, nil
			}
			return "worker-0\n", nil
		}
		if strings.HasPrefix(command, "scontrol show node ") {
			r.commands = append(r.commands, command)
			r.slurmReads++
			if len(r.slurmNodes) > 0 {
				name := strings.Trim(strings.TrimPrefix(command, "scontrol show node "), "'")
				for _, node := range strings.Split(r.slurmNodes[min(r.slurmReads-1, len(r.slurmNodes)-1)], "\n") {
					if strings.HasPrefix(node, "NodeName="+name+" ") {
						return node, nil
					}
				}
				return "", fmt.Errorf("unexpected Slurm node query %q", command)
			}
			return "NodeName=worker-0 State=IDLE SlurmdStartTime=2026-11-23T00:00:00", nil
		}
		return r.Jail().Run(ctx, command)
	})
}

func (r *privacyCleanupRuntime) Worker(framework.WorkerInfo) framework.CommandScope {
	r.workerSSHCalls++
	return framework.NewCommandScope(func(context.Context, string) (string, error) {
		return "", errors.New("worker SSH is unavailable during rollout")
	})
}

func (r *privacyCleanupRuntime) WorkerPod(pod framework.WorkerPodInfo) framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		r.workerCommands = append(r.workerCommands, pod.PodName+": "+command)
		if r.workerExecFailures > 0 {
			r.workerExecFailures--
			return "", &framework.CommandError{Name: "kubectl", Args: []string{"exec", pod.PodName},
				Err: errors.New("exit status 1"), Stderr: `error: unable to upgrade connection: container not found ("slurmd")`}
		}
		return r.identities, nil
	})
}

func (r *privacyCleanupRuntime) Kubectl() framework.ArgsScope {
	return framework.NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		r.kubectlCommands = append(r.kubectlCommands, strings.Join(args, " "))
		if args[0] == "exec" {
			if strings.HasPrefix(args[len(args)-1], "getent passwd ") {
				return r.identities, nil
			}
			if r.privateData == nil {
				return "", nil
			}
			return "PrivateData=" + *r.privateData, nil
		}
		if args[0] == "patch" {
			patch := args[len(args)-1]
			r.patches = append(r.patches, patch)
			var data struct {
				Spec struct {
					SlurmNodes struct {
						Accounting struct {
							SlurmdbdConfig struct{ PrivateData *string }
						}
					}
				}
			}
			if err := json.Unmarshal([]byte(patch), &data); err != nil {
				return "", err
			}
			r.privateData = data.Spec.SlurmNodes.Accounting.SlurmdbdConfig.PrivateData
			r.ready = true
			r.pendingClients = r.clientsPendingAfterPatch
			return "", nil
		}
		deployment, pod := privacyReadyWorkload()
		if !r.ready {
			deployment.Status.ReadyReplicas = 0
			pod.Status.Conditions = nil
		}
		var object any
		switch args[1] {
		case "slurmcluster":
			config := make(map[string]any)
			if r.privateData != nil {
				config["privateData"] = *r.privateData
			}
			object = map[string]any{"spec": map[string]any{"slurmNodes": map[string]any{
				"accounting": map[string]any{"slurmdbdConfig": config},
			}}}
		case "deployments":
			object = appsv1.DeploymentList{Items: []appsv1.Deployment{deployment}}
		case "pods":
			pod.UID = types.UID(fmt.Sprintf("pod-%d", len(r.patches)))
			if strings.Contains(strings.Join(args, " "), "--ignore-not-found") {
				r.workerReads++
				if len(r.workerSnapshots) == 0 {
					return "", errors.New("unexpected worker pod read")
				}
				object = r.workerSnapshots[min(r.workerReads-1, len(r.workerSnapshots)-1)]
			} else {
				if strings.Contains(strings.Join(args, " "), "component=login") && r.pendingClients > 0 {
					r.pendingClients--
					pod.Status.Conditions = nil
				}
				object = corev1.PodList{Items: []corev1.Pod{pod}}
			}
		default:
			return "", fmt.Errorf("unexpected kubectl args %q", args)
		}
		data, err := json.Marshal(object)
		return string(data), err
	})
}

func newPrivacyCleanupTest(r *privacyCleanupRuntime) *Accounting {
	s := NewAccounting(&framework.ClusterInfo{SlurmClusterName: "soperator"}, r, framework.NewSlurmClient(r), framework.NewKubectlClient(r), nil)
	s.privacy = newAccountingPrivacyState("e2epunit")
	s.clusterName = "soperator"
	return s
}

func TestAccountingPrivacyCleanupRestoresAbsentFieldAfterCancelledPartialSetup(t *testing.T) {
	runtime := &privacyCleanupRuntime{privateData: stringPointer("jobs,usage,users")}
	s := newPrivacyCleanupTest(runtime)
	state := s.privacy
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s.CleanupAndReset(ctx)
	assert.Nil(t, s.privacy)
	assert.Nil(t, runtime.privateData)
	require.NotEmpty(t, runtime.patches)
	assert.Contains(t, runtime.patches[0], `"privateData":null`)
	for _, user := range newAccountingPrivacyState("e2epunit").Users {
		assert.Contains(t, strings.Join(runtime.commands, "\n"), "deluser --remove-home '"+user.Name+"'")
	}
	assert.Empty(t, s.clusterName)
	require.NotEmpty(t, state.Observations)
	assert.Empty(t, state.Observations[len(state.Observations)-1].Error)
}

func TestAccountingPrivacyCleanupContinuesAndCanRetry(t *testing.T) {
	runtime := &privacyCleanupRuntime{privateData: stringPointer("jobs,usage,users"), ready: true, failUser: "e2epunitalice"}
	s := newPrivacyCleanupTest(runtime)
	require.ErrorContains(t, s.restoreAccountingPrivacy(t.Context()), "injected error")
	assert.Nil(t, runtime.privateData)
	assert.Contains(t, strings.Join(runtime.commands, "\n"), "deluser --remove-home 'e2epunitnodb'")
	require.NotNil(t, s.privacy)
	runtime.failUser = ""
	require.NoError(t, s.restoreAccountingPrivacy(t.Context()))
	assert.Nil(t, s.privacy)
	commands := len(runtime.commands)
	require.NoError(t, s.restoreAccountingPrivacy(t.Context()))
	assert.Len(t, runtime.commands, commands)
}

func TestAccountingPrivacyCleanupRestoresEveryOriginalSetting(t *testing.T) {
	for _, tc := range []struct {
		name     string
		original *string
	}{
		{name: "absent"},
		{name: "explicitly empty", original: stringPointer("")},
		{name: "other flags", original: stringPointer("events,users")},
		{name: "already private", original: stringPointer("jobs,usage,users")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := &privacyCleanupRuntime{privateData: stringPointer("jobs,usage,users"), ready: true}
			s := newPrivacyCleanupTest(runtime)
			s.privacy.OriginalPrivateData = tc.original
			require.NoError(t, s.restoreAccountingPrivacy(t.Context()))
			assert.Equal(t, tc.original, runtime.privateData)
			assert.Nil(t, s.privacy)
		})
	}
}

func TestAccountingPrivacyQueriesUseTheRequestingUser(t *testing.T) {
	runtime := &privacyCleanupRuntime{}
	s := newPrivacyCleanupTest(runtime)
	_, err := s.privacyQuery(t.Context(), "alice", "sacct -X -a -j 123")
	require.NoError(t, err)
	assert.Equal(t, []string{"su - 'alice' -c 'sacct -X -a -j 123'"}, runtime.commands)
}

func privacyTestWorker(ready bool, uid string) corev1.Pod {
	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker-0", UID: types.UID(uid)},
		Spec: corev1.PodSpec{Hostname: "worker-0"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	if ready {
		pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	}
	return pod
}

func TestAccountingPrivacyNSSRecoversWhenWorkerIsReplacedDuringExec(t *testing.T) {
	runtime := &privacyCleanupRuntime{ready: true, waitAttempts: 5, workerExecFailures: 1,
		workerSnapshots: []corev1.PodList{
			{Items: []corev1.Pod{privacyTestWorker(true, "old-worker")}},
			{},
			{Items: []corev1.Pod{privacyTestWorker(false, "new-worker")}},
			{Items: []corev1.Pod{privacyTestWorker(true, "new-worker")}},
		}}
	s := newPrivacyCleanupTest(runtime)
	s.worker.Name = "worker-0"
	for i, user := range s.privacy.Users {
		runtime.identities += fmt.Sprintf("%s:x:%d:%d::%s:/bin/bash\n", user.Name, 1000+i, 1000+i, user.Home)
	}
	require.NoError(t, s.checkAccountingPrivacyIdentities(t.Context()))
	assert.Zero(t, runtime.workerSSHCalls)
	assert.Equal(t, 4, runtime.workerReads)
	require.Len(t, runtime.workerCommands, 2)
	assert.Equal(t, "worker-0: chroot /mnt/jail bash -lc "+framework.ShellQuote("getent passwd "+strings.Join(s.privacy.userNames(), " ")),
		runtime.workerCommands[1])
	for _, command := range runtime.kubectlCommands {
		if strings.Contains(command, "--ignore-not-found") {
			assert.Equal(t, "get pods -n soperator --ignore-not-found -o json worker-0", command)
		}
	}
	assert.Equal(t, accountingPrivacyIdentity{UID: "1000", GID: "1000"}, s.privacy.Users[0].Identity)
}

func TestAccountingPrivacyCleanupWaitsForLoginAfterRestoringConfiguration(t *testing.T) {
	runtime := &privacyCleanupRuntime{privateData: stringPointer("jobs,usage,users"), ready: true,
		waitAttempts: 5, clientsPendingAfterPatch: 3,
		workerSnapshots: []corev1.PodList{{Items: []corev1.Pod{privacyTestWorker(true, "new-worker")}}}}
	s := newPrivacyCleanupTest(runtime)
	s.worker.Name = "worker-0"
	state := s.privacy
	require.NoError(t, s.restoreAccountingPrivacy(t.Context()))
	assert.Nil(t, s.privacy)
	assert.Nil(t, runtime.privateData)
	assert.Zero(t, runtime.prematureCommands)
	assert.Zero(t, runtime.pendingClients)
	assert.Positive(t, runtime.workerReads)
	for _, user := range state.Users {
		assert.Equal(t, 1, strings.Count(strings.Join(runtime.commands, "\n"), "deluser --remove-home '"+user.Name+"'"))
		assert.Contains(t, strings.Join(runtime.commands, "\n"), "test ! -e '"+user.Home+"'")
	}
	assert.Contains(t, runtime.commands, "getent passwd "+strings.Join(state.userNames(), " ")+` || [ "$?" -eq 2 ]`)
	assert.Contains(t, runtime.commands, "getent group "+strings.Join(state.userNames(), " ")+` || [ "$?" -eq 2 ]`)
	assert.Empty(t, state.Observations[len(state.Observations)-1].Error)
}

func TestAccountingPrivacyCleanupWaitsForAllWorkers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state string
		reads int
	}{
		{"drained worker", "IDLE+DRAIN+NOT_RESPONDING", 3},
		{"healthy suspended worker", "IDLE+POWERED_DOWN", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const readyWorker = "NodeName=worker-0 State=IDLE\n"
			runtime := &privacyCleanupRuntime{ready: true, waitAttempts: 2,
				workerSnapshots: []corev1.PodList{{Items: []corev1.Pod{privacyTestWorker(true, "worker-uid")}}},
				slurmNodeNames:  "worker-0\nworker-1\n",
				slurmNodes: []string{
					readyWorker + "NodeName=worker-1 State=" + tc.state,
					readyWorker + "NodeName=worker-1 State=" + tc.state,
					readyWorker + "NodeName=worker-1 State=IDLE",
				}}
			s := newPrivacyCleanupTest(runtime)
			s.worker.Name = "worker-0"
			require.NoError(t, s.restoreAccountingPrivacy(t.Context()))
			assert.Nil(t, s.privacy)
			assert.Equal(t, tc.reads, runtime.slurmReads)
			commands := strings.Join(runtime.commands, "\n")
			assert.Greater(t, strings.Index(commands, "scontrol show node "), strings.LastIndex(commands, "getent group "))
		})
	}
}

func TestAccountingPrivacyCleanupReportsUnrecoveredWorker(t *testing.T) {
	runtime := &privacyCleanupRuntime{ready: true, waitAttempts: 2,
		workerSnapshots: []corev1.PodList{{Items: []corev1.Pod{privacyTestWorker(true, "worker-uid")}}},
		slurmNodes:      []string{"NodeName=worker-0 State=IDLE+DRAIN Reason=soperator rolling update"}}
	s := newPrivacyCleanupTest(runtime)
	s.worker.Name = "worker-0"
	ctx := framework.WithWorkerCleanup(t.Context())
	s.CleanupAndReset(ctx)
	require.ErrorContains(t, framework.FinishWorkerReservation(ctx, runtime), "wait for Slurm workers to recover")
	assert.Equal(t, 2, runtime.slurmReads)
}
