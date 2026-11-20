package sharedsteps

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	accountingPrivacyRolloutTimeout = 5 * time.Minute
	accountingPrivacyUserTimeout    = 2 * time.Minute
	accountingPrivacyFields         = "JobIDRaw,User%64,Account%64,JobName%96,State,AssocID,WorkDir%512,SubmitLine%2048"
)

type accountingPrivacyUser struct {
	Role     string
	Name     string
	Account  string
	Home     string
	Identity accountingPrivacyIdentity
}

type accountingPrivacyJob struct {
	Job    framework.SbatchJob
	Record accountingPrivacyJobRecord
	Marker string
}

type accountingPrivacyObservation struct {
	Actor   string
	Command string
	Output  string
	Error   string `json:",omitempty"`
}

type accountingPrivacyState struct {
	Prefix              string
	OriginalPrivateData *string
	Users               []accountingPrivacyUser
	Accounts            []string
	Jobs                []accountingPrivacyJob
	Observations        []accountingPrivacyObservation
}

func (s *Accounting) registerPrivacySteps(sc *godog.ScenarioContext) {
	sc.Step(`^independent Linux users and accounts are ready for accounting privacy checks$`, s.prepareAccountingPrivacy)
	sc.Step(`^the users complete jobs with different working directories and submit commands$`, s.submitAccountingPrivacyJobs)
	sc.Step(`^the test jobs are visible while accounting privacy is disabled$`, s.accountingPrivacyDisabledView)
	sc.Step(`^accounting privacy is enabled for jobs, usage, and users$`, s.enableAccountingPrivacy)
	sc.Step(`^accounting records follow user and account permissions$`, s.checkAccountingPrivacy)
	sc.Step(`^the accounting service is restarted$`, s.restartAccountingPrivacy)
	sc.Step(`^accounting privacy is disabled again$`, s.disableAccountingPrivacy)
	sc.Step(`^the original accounting configuration and privacy fixtures are restored$`, s.restoreAccountingPrivacy)
}

func newAccountingPrivacyState(prefix string) *accountingPrivacyState {
	p := &accountingPrivacyState{Prefix: prefix}
	p.Accounts = append(p.Accounts, prefix+"a", prefix+"b")
	for _, role := range []string{"alice", "bob", "carol", "coord", "role", "nodb"} {
		account := p.Accounts[0]
		if role == "carol" {
			account = p.Accounts[1]
		} else if role == "nodb" {
			account = ""
		}
		name := prefix + role
		p.Users = append(p.Users, accountingPrivacyUser{
			Role: role, Name: name, Account: account, Home: "/opt/soperator-home/" + name,
		})
	}
	return p
}

func (p *accountingPrivacyState) userNames() []string {
	var names []string
	for _, user := range p.Users {
		names = append(names, user.Name)
	}
	return names
}

func (p *accountingPrivacyState) record(ctx context.Context, actor, command, output string, commandErr error) error {
	observation := accountingPrivacyObservation{Actor: actor, Command: command, Output: output}
	if commandErr != nil {
		observation.Error = commandErr.Error()
	}
	p.Observations = append(p.Observations, observation)
	if artifacts, ok := framework.ScenarioArtifacts(ctx); ok {
		data, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal accounting privacy evidence: %w", err)
		}
		return artifacts.WriteRunnerFile("accounting-privacy.json", string(data)+"\n")
	}
	return nil
}

func (s *Accounting) privacyQuery(ctx context.Context, actor, command string) (string, error) {
	query := command
	if actor != "root" {
		query = runAsLoginUser(actor, command)
	}
	output, err := s.runtime.Jail().Run(ctx, query)
	artifactErr := s.privacy.record(ctx, actor, command, output, err)
	return output, errors.Join(err, artifactErr)
}

func (s *Accounting) prepareAccountingPrivacy(ctx context.Context) error {
	if s.clusterName == "" {
		return fmt.Errorf("select a registered accounting cluster before creating privacy fixtures")
	}
	if err := s.selectAccountingPrivacyWorker(ctx); err != nil {
		return err
	}
	if _, _, err := s.accountingPrivacyWorkload(ctx, ""); err != nil {
		return err
	}
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("generate accounting privacy fixture names: %w", err)
	}
	p := newAccountingPrivacyState(fmt.Sprintf("e2ep%x", nonce))
	if err := s.requireUnusedAccountingPrivacyFixtures(ctx, p); err != nil {
		return err
	}
	original, err := s.readAccountingPrivateData(ctx)
	if err != nil {
		return err
	}
	p.OriginalPrivateData = original
	s.privacy = p
	if err := p.record(ctx, "root", "record original PrivateData and fixture ownership", "", nil); err != nil {
		return err
	}
	if err := s.disableAccountingPrivacy(ctx); err != nil {
		return err
	}
	for _, user := range p.Users {
		if err := createSSHTestUser(ctx, s.runtime, user.Name,
			[]string{"--without-sudo", "--without-docker", "--home=" + user.Home}); err != nil {
			return err
		}
		if err := ensureUnprivilegedSSHTestUser(ctx, s.runtime, user.Name); err != nil {
			return err
		}
	}
	for _, account := range p.Accounts {
		command := fmt.Sprintf("sacctmgr -i add account name=%s cluster=%s description=%s organization=e2e",
			framework.ShellQuote(account), framework.ShellQuote(s.clusterName), framework.ShellQuote("Soperator accounting privacy"))
		if _, err := s.privacyQuery(ctx, "root", command); err != nil {
			return fmt.Errorf("create privacy account %s: %w", account, err)
		}
	}
	for _, user := range p.Users {
		if user.Account == "" {
			continue
		}
		command := fmt.Sprintf("sacctmgr -i add user name=%s account=%s cluster=%s defaultaccount=%s adminlevel=None",
			framework.ShellQuote(user.Name), framework.ShellQuote(user.Account),
			framework.ShellQuote(s.clusterName), framework.ShellQuote(user.Account))
		if _, err := s.privacyQuery(ctx, "root", command); err != nil {
			return fmt.Errorf("associate privacy user %s: %w", user.Name, err)
		}
		if user.Role == "coord" {
			command = fmt.Sprintf("sacctmgr -i add coordinator names=%s accounts=%s",
				framework.ShellQuote(user.Name), framework.ShellQuote(user.Account))
			if _, err := s.privacyQuery(ctx, "root", command); err != nil {
				return fmt.Errorf("create account coordinator %s: %w", user.Name, err)
			}
		}
	}
	if err := s.checkAccountingPrivacyIdentities(ctx); err != nil {
		return err
	}
	// Load fixture associations explicitly; their asynchronous propagation is a separate workflow.
	if _, err := s.runtime.Controller().Run(ctx, "scontrol reconfigure"); err != nil {
		return fmt.Errorf("load privacy associations on the controller: %w", err)
	}
	command := "scontrol show assoc_mgr flags=assoc users=" + framework.ShellQuote(strings.Join(p.userNames(), ",")) +
		" accounts=" + framework.ShellQuote(strings.Join(p.Accounts, ","))
	return s.runtime.WaitFor(ctx, "privacy associations loaded by slurmctld", accountingPrivacyUserTimeout,
		framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			output, err := s.runtime.Controller().Run(waitCtx, command)
			if artifactErr := p.record(waitCtx, "controller", command, output, err); artifactErr != nil {
				return false, artifactErr
			}
			if err != nil {
				return false, err
			}
			for _, user := range p.Users {
				if user.Account == "" {
					continue
				}
				if !strings.Contains(output, "UserName="+user.Name+"("+user.Identity.UID+")") {
					return false, fmt.Errorf("wait for controller association of %s uid=%s:\n%s", user.Name, user.Identity.UID, output)
				}
			}
			return true, nil
		})
}

func (s *Accounting) selectAccountingPrivacyWorker(ctx context.Context) error {
	nodeSets, err := s.kubectl.NodeSets(ctx, s.info.SlurmClusterName)
	if err != nil {
		return err
	}
	var configured int
	for _, nodeSet := range nodeSets {
		configured += nodeSet.Replicas
	}
	if configured == 0 {
		return fmt.Errorf("require a configured worker for accounting privacy checks")
	}
	return s.runtime.WaitFor(ctx, "an active idle worker for accounting privacy", accountingPrivacyUserTimeout,
		framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			output, err := s.runtime.Jail().Run(waitCtx, partitionNodesCommand("main"))
			if err != nil {
				return false, err
			}
			nodes, err := parsePartitionNodes(output)
			if err != nil {
				return false, &framework.PermanentError{Err: err}
			}
			for _, node := range nodes {
				if strings.EqualFold(node.State, "idle") {
					pods, err := s.kubectl.WorkerPodsForSlurmNodes(waitCtx, []string{node.Name})
					if err != nil {
						return false, err
					}
					if len(pods) != 1 || !pods[0].Ready {
						continue
					}
					s.worker = framework.WorkerInfo{Name: node.Name}
					return true, nil
				}
			}
			return false, fmt.Errorf("require an active idle worker, observed:\n%s", output)
		})
}

func (s *Accounting) requireUnusedAccountingPrivacyFixtures(ctx context.Context, p *accountingPrivacyState) error {
	names := strings.Join(p.userNames(), " ")
	for _, database := range []string{"passwd", "group"} {
		output, err := s.runtime.Jail().Run(ctx, "getent "+database+" "+names+` || [ "$?" -eq 2 ]`)
		if err != nil {
			return fmt.Errorf("check unused privacy %s names: %w", database, err)
		}
		if strings.TrimSpace(output) != "" {
			return fmt.Errorf("require unused privacy %s names, observed %s", database, output)
		}
	}
	for _, command := range []string{
		"sacctmgr -nP show user where name=" + framework.ShellQuote(strings.Join(p.userNames(), ",")) + " format=User%64",
		"sacctmgr -nP show account where name=" + framework.ShellQuote(strings.Join(p.Accounts, ",")) + " format=Account%64",
	} {
		output, err := s.runtime.Jail().Run(ctx, command)
		if err != nil {
			return fmt.Errorf("check unused privacy DB records: %w", err)
		}
		if strings.TrimSpace(output) != "" {
			return fmt.Errorf("require unused privacy DB records, observed %s", output)
		}
	}
	for _, user := range p.Users {
		if _, err := s.runtime.Jail().Run(ctx, "test ! -e "+framework.ShellQuote(user.Home)); err != nil {
			return fmt.Errorf("require unused privacy home %s: %w", user.Home, err)
		}
	}
	return nil
}

func (s *Accounting) checkAccountingPrivacyIdentities(ctx context.Context) error {
	p := s.privacy
	command := "getent passwd " + strings.Join(p.userNames(), " ")
	output, err := s.privacyQuery(ctx, "root", command)
	if err != nil {
		return err
	}
	expected, err := parseAccountingPrivacyIdentities(output)
	if err != nil || len(expected) != len(p.Users) {
		return fmt.Errorf("read all privacy users from the jail: output=%q error=%v", output, err)
	}
	for i := range p.Users {
		p.Users[i].Identity = expected[p.Users[i].Name]
	}
	accounting := framework.NewCommandScope(func(scopeCtx context.Context, cmd string) (string, error) {
		pod, err := s.kubectl.ReadyWorkloadPodName(scopeCtx, s.info.SlurmClusterName, "accounting")
		if err != nil {
			return "", err
		}
		return s.runtime.Kubectl().Run(scopeCtx, "exec", "-n", framework.SoperatorNamespace, pod,
			"-c", "accounting", "--", "bash", "-lc", cmd)
	})
	worker := framework.NewCommandScope(func(scopeCtx context.Context, cmd string) (string, error) {
		pods, err := s.kubectl.WorkerPodsForSlurmNodes(scopeCtx, []string{s.worker.Name})
		if err != nil {
			return "", err
		}
		if len(pods) != 1 || !pods[0].Ready {
			return "", fmt.Errorf("wait for Ready worker pod %s before checking NSS identities", s.worker.Name)
		}
		return s.runtime.WorkerPod(pods[0]).Run(scopeCtx, "chroot /mnt/jail bash -lc "+framework.ShellQuote(cmd))
	})
	for _, target := range []struct {
		name  string
		scope framework.CommandScope
	}{{"controller", s.runtime.Controller()}, {s.worker.Name, worker}, {"accounting", accounting}} {
		if err := s.runtime.WaitFor(ctx, "matching privacy user UIDs/GIDs on "+target.name,
			accountingPrivacyUserTimeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
				output, err := target.scope.Run(waitCtx, command)
				if artifactErr := p.record(waitCtx, target.name, command, output, err); artifactErr != nil {
					return false, artifactErr
				}
				if err != nil {
					return false, err
				}
				observed, err := parseAccountingPrivacyIdentities(output)
				if err != nil {
					return false, err
				}
				if !maps.Equal(expected, observed) {
					return false, fmt.Errorf("require NSS identities %v on %s, observed %v", expected, target.name, observed)
				}
				return true, nil
			}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Accounting) submitAccountingPrivacyJobs(ctx context.Context) error {
	p := s.privacy
	for _, user := range p.Users[:3] {
		workdir := path.Join(user.Home, "work")
		if _, err := s.privacyQuery(ctx, user.Name, "mkdir -p "+framework.ShellQuote(workdir)); err != nil {
			return err
		}
		outputDir := workdir
		if artifacts, ok := framework.ScenarioArtifacts(ctx); ok {
			outputDir = path.Join(artifacts.JailDir, user.Role)
			if _, err := s.runtime.Jail().Run(ctx, userOwnedDirCommand(user.Name, outputDir)); err != nil {
				return err
			}
		}
		marker := p.Prefix + "_" + user.Role
		job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
			JobName: p.Prefix + "-" + user.Role, Nodes: 1, Nodelist: []string{s.worker.Name},
			RunAsUser: user.Name, OutputDir: outputDir, TimeLimit: 3 * time.Minute,
			Args: []string{"--account=" + user.Account, "--partition=main", "--ntasks=1", "--cpus-per-task=1", "--mem=64M", "--chdir=" + workdir},
			Wrap: "printf '%s\\n' " + marker + "; sleep 2",
		})
		p.Jobs = append(p.Jobs, accountingPrivacyJob{Job: job, Marker: marker,
			Record: accountingPrivacyJobRecord{ID: job.ID, User: user.Name, Account: user.Account, Name: job.JobName, WorkDir: workdir}})
		if artifactErr := p.record(ctx, user.Name, "submit privacy job", job.ID, err); artifactErr != nil {
			return artifactErr
		}
		if err != nil {
			return err
		}
	}
	for _, job := range p.Jobs {
		if err := framework.WaitForJobSucceeded(ctx, s.runtime, s.slurm, job.Job, 3*time.Minute); err != nil {
			return err
		}
	}
	return s.runtime.WaitFor(ctx, "complete privacy accounting job records", accountingPrivacyUserTimeout,
		framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			output, err := s.privacyQuery(waitCtx, "root", s.accountingPrivacyJobQuery())
			if err != nil {
				return false, err
			}
			jobs, err := captureAccountingPrivacyJobs(output, p.Jobs)
			if err != nil {
				return false, err
			}
			p.Jobs = jobs
			return true, nil
		})
}

func (s *Accounting) accountingPrivacyJobQuery() string {
	var ids []string
	for _, job := range s.privacy.Jobs {
		ids = append(ids, job.Job.ID)
	}
	return "sacct -X -nP -a -j " + framework.ShellQuote(strings.Join(ids, ",")) + " --format=" + accountingPrivacyFields
}

func (s *Accounting) accountingPrivacyDisabledView(ctx context.Context) error {
	var expected []accountingPrivacyJobRecord
	for _, job := range s.privacy.Jobs {
		expected = append(expected, job.Record)
	}
	for _, actor := range []string{"root", s.privacy.Users[0].Name} {
		output, err := s.privacyQuery(ctx, actor, s.accountingPrivacyJobQuery())
		if err != nil {
			return err
		}
		if err := validateAccountingPrivacyJobs(output, expected); err != nil {
			return fmt.Errorf("verify privacy-disabled jobs as %s: %w", actor, err)
		}
	}
	return nil
}

func (s *Accounting) enableAccountingPrivacy(ctx context.Context) error {
	value := "jobs,usage,users"
	return s.setAccountingPrivateData(ctx, &value)
}

func (s *Accounting) disableAccountingPrivacy(ctx context.Context) error {
	return s.setAccountingPrivateData(ctx, nil)
}

func (s *Accounting) checkAccountingPrivacy(ctx context.Context) error {
	p := s.privacy
	for _, user := range p.Users {
		if user.Role == "role" {
			continue
		}
		if err := s.checkAccountingPrivacyActor(ctx, user.Name, user.Account, user.Role == "coord", false); err != nil {
			return err
		}
	}
	if err := s.checkAccountingPrivacyActor(ctx, "root", "", false, true); err != nil {
		return err
	}
	roleUser := p.Users[4]
	for _, role := range []string{"Operator", "Admin"} {
		command := fmt.Sprintf("sacctmgr -i modify user where name=%s set adminlevel=%s", framework.ShellQuote(roleUser.Name), role)
		if _, err := s.privacyQuery(ctx, "root", command); err != nil {
			return err
		}
		if err := s.checkAccountingPrivacyActor(ctx, roleUser.Name, roleUser.Account, false, true); err != nil {
			return fmt.Errorf("verify %s accounting permissions: %w", role, err)
		}
	}
	_, err := s.privacyQuery(ctx, "root", "sacctmgr -i modify user where name="+framework.ShellQuote(roleUser.Name)+" set adminlevel=None")
	return err
}

func (s *Accounting) checkAccountingPrivacyActor(ctx context.Context, actor, account string, coordinator, privileged bool) error {
	p := s.privacy
	jobs, users, associations := p.permissions(actor, account, coordinator, privileged)
	if err := s.runtime.WaitFor(ctx, "accounting permissions for "+actor, accountingPrivacyUserTimeout,
		framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			output, err := s.privacyQuery(waitCtx, actor, s.accountingPrivacyJobQuery())
			if err != nil {
				return false, err
			}
			if err := validateAccountingPrivacyJobs(output, jobs); err != nil {
				return false, err
			}
			// A user absent from SlurmDB has no accounting identity to query with sacctmgr.
			if account == "" && !privileged {
				return true, nil
			}
			output, err = s.privacyQuery(waitCtx, actor, "sacctmgr -nP show user where name="+
				framework.ShellQuote(strings.Join(p.userNames(), ","))+" format=User%64")
			if err != nil {
				return false, err
			}
			if err := validateAccountingPrivacyRows(output, 1, users); err != nil {
				return false, err
			}
			output, err = s.privacyQuery(waitCtx, actor, "sacctmgr -nP show assoc where cluster="+framework.ShellQuote(s.clusterName)+
				" accounts="+framework.ShellQuote(strings.Join(p.Accounts, ","))+" users="+
				framework.ShellQuote(strings.Join(p.userNames(), ","))+" format=User%64,Account%64")
			if err != nil {
				return false, err
			}
			return true, validateAccountingPrivacyRows(output, 2, associations)
		}); err != nil {
		return err
	}
	if !privileged && !coordinator && actor == p.Users[0].Name {
		for _, query := range []string{
			s.accountingPrivacyJobQuery() + " --user=" + framework.ShellQuote(p.Users[1].Name),
			"sacct -X -nP -a -j " + framework.ShellQuote(p.Jobs[1].Job.ID) + " --format=" + accountingPrivacyFields,
		} {
			output, err := s.privacyQuery(ctx, actor, query)
			if err != nil {
				return err
			}
			if err := validateAccountingPrivacyJobs(output, nil); err != nil {
				return fmt.Errorf("deny explicit foreign job request by %s: %w", actor, err)
			}
		}
	}
	return nil
}

func (s *Accounting) readAccountingPrivateData(ctx context.Context) (*string, error) {
	var cluster struct {
		Spec struct {
			SlurmNodes struct {
				Accounting struct {
					SlurmdbdConfig struct {
						PrivateData *string `json:"privateData"`
					} `json:"slurmdbdConfig"`
				} `json:"accounting"`
			} `json:"slurmNodes"`
		} `json:"spec"`
	}
	if err := s.kubectl.GetJSON(ctx, &cluster, "get", "slurmcluster", s.info.SlurmClusterName,
		"-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
		return nil, err
	}
	return cluster.Spec.SlurmNodes.Accounting.SlurmdbdConfig.PrivateData, nil
}

func accountingPrivacyPatch(value *string) ([]byte, error) {
	return json.Marshal(map[string]any{"spec": map[string]any{"slurmNodes": map[string]any{
		"accounting": map[string]any{"slurmdbdConfig": map[string]any{"privateData": value}},
	}}})
}

func (s *Accounting) setAccountingPrivateData(ctx context.Context, value *string) error {
	current, err := s.readAccountingPrivateData(ctx)
	if err != nil {
		return err
	}
	_, pod, workloadErr := s.accountingPrivacyWorkload(ctx, "")
	if workloadErr != nil {
		// Restoration must still patch the CR when an interrupted rollout left no Ready pod.
		s.runtime.Logf("Accounting workload before PrivateData patch: %v", workloadErr)
	}
	patch, err := accountingPrivacyPatch(value)
	if err != nil {
		return fmt.Errorf("marshal accounting PrivateData patch: %w", err)
	}
	if _, err := s.runtime.Kubectl().Run(ctx, "patch", "slurmcluster", s.info.SlurmClusterName,
		"-n", framework.SoperatorNamespace, "--type=merge", "-p", string(patch)); err != nil {
		return fmt.Errorf("patch accounting PrivateData: %w", err)
	}
	var previousUID types.UID
	if accountingPrivacyFlags(current) != accountingPrivacyFlags(value) {
		previousUID = pod.UID
	}
	if err := s.waitAccountingPrivacy(ctx, value, previousUID); err != nil {
		return err
	}
	return s.waitAccountingPrivacyClients(ctx)
}

func (s *Accounting) waitAccountingPrivacyClients(ctx context.Context) error {
	return s.runtime.WaitFor(ctx, "ready accounting privacy client pods", accountingPrivacyRolloutTimeout,
		framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			for _, component := range []string{"login", "controller"} {
				if _, err := s.kubectl.ReadyWorkloadPodName(waitCtx, s.info.SlurmClusterName, component); err != nil {
					return false, err
				}
			}
			if s.worker.Name != "" {
				pods, err := s.kubectl.WorkerPodsForSlurmNodes(waitCtx, []string{s.worker.Name})
				if err != nil {
					return false, err
				}
				if len(pods) != 1 || !pods[0].Ready {
					return false, fmt.Errorf("wait for Ready worker pod %s after accounting configuration update", s.worker.Name)
				}
			}
			return true, nil
		})
}

func (s *Accounting) accountingPrivacyWorkload(ctx context.Context, previousUID types.UID) (appsv1.Deployment, corev1.Pod, error) {
	selector := "app.kubernetes.io/instance=" + s.info.SlurmClusterName + ",app.kubernetes.io/component=accounting"
	var deployments appsv1.DeploymentList
	if err := s.kubectl.GetJSON(ctx, &deployments, "get", "deployments", "-n", framework.SoperatorNamespace, "-l", selector, "-o", "json"); err != nil {
		return appsv1.Deployment{}, corev1.Pod{}, err
	}
	if len(deployments.Items) != 1 {
		return appsv1.Deployment{}, corev1.Pod{}, fmt.Errorf("require one accounting Deployment, observed %d", len(deployments.Items))
	}
	deployment := deployments.Items[0]
	var pods corev1.PodList
	if err := s.kubectl.GetJSON(ctx, &pods, "get", "pods", "-n", framework.SoperatorNamespace, "-l", selector, "-o", "json"); err != nil {
		return deployment, corev1.Pod{}, err
	}
	pod, err := accountingPrivacyReadyPod(deployment, pods.Items, previousUID)
	return deployment, pod, err
}

func (s *Accounting) waitAccountingPrivacy(ctx context.Context, value *string, previousUID types.UID) error {
	expected := accountingPrivacyFlags(value)
	return s.runtime.WaitFor(ctx, "accounting rollout with PrivateData="+expected, accountingPrivacyRolloutTimeout,
		framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
			_, pod, err := s.accountingPrivacyWorkload(waitCtx, previousUID)
			if err != nil {
				return false, err
			}
			current, err := s.readAccountingPrivateData(waitCtx)
			if err != nil {
				return false, err
			}
			if (value == nil) != (current == nil) || (value != nil && current != nil && *value != *current) {
				return false, fmt.Errorf("wait for PrivateData field restoration: expected %v observed %v", value, current)
			}
			// Slurm 26.05's show config builds PrivateData from the client's slurm.conf.
			// Check the new pod's file here; authenticated queries below prove daemon behavior.
			output, err := s.runtime.Kubectl().Run(waitCtx, "exec", "-n", pod.Namespace, pod.Name,
				"-c", "accounting", "--", "awk", `/^[[:space:]]*PrivateData[[:space:]]*=/ { print }`, "/etc/slurm/slurmdbd.conf")
			if err != nil {
				return false, err
			}
			actual, err := accountingPrivacyConfigFlags(output)
			if err != nil {
				return false, err
			}
			if actual != expected {
				return false, fmt.Errorf("require slurmdbd.conf PrivateData=%q on pod %s/%s, observed %q", expected, pod.Name, pod.UID, actual)
			}
			return true, s.privacy.record(waitCtx, "accounting", "verify slurmdbd.conf PrivateData", actual+" pod="+pod.Name+" uid="+string(pod.UID), nil)
		})
}

func (s *Accounting) restartAccountingPrivacy(ctx context.Context) error {
	deployment, pod, err := s.accountingPrivacyWorkload(ctx, "")
	if err != nil {
		return err
	}
	owner := metav1.GetControllerOf(&pod)
	if owner == nil || owner.Kind != "ReplicaSet" {
		return fmt.Errorf("require a Deployment-owned accounting pod, observed owner=%v", owner)
	}
	var replicaSet appsv1.ReplicaSet
	if err := s.kubectl.GetJSON(ctx, &replicaSet, "get", "replicaset", owner.Name, "-n", pod.Namespace, "-o", "json"); err != nil {
		return err
	}
	deploymentOwner := metav1.GetControllerOf(&replicaSet)
	if replicaSet.UID != owner.UID || deploymentOwner == nil || deploymentOwner.UID != deployment.UID {
		return fmt.Errorf("verify accounting pod %s belongs to Deployment %s", pod.Name, deployment.Name)
	}
	if _, err := s.runtime.Kubectl().Run(ctx, "delete", "pod", pod.Name, "-n", pod.Namespace, "--wait=false"); err != nil {
		return fmt.Errorf("restart accounting pod %s: %w", pod.Name, err)
	}
	value := "jobs,usage,users"
	if err := s.waitAccountingPrivacy(ctx, &value, pod.UID); err != nil {
		return err
	}
	return s.checkAccountingPrivacyIdentities(ctx)
}

func (s *Accounting) restoreAccountingPrivacy(ctx context.Context) error {
	p := s.privacy
	if p == nil {
		return nil
	}
	var failures []error
	for _, phase := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"cancel privacy jobs", s.cancelAccountingPrivacyJobs},
		{"restore original PrivateData", func(phaseCtx context.Context) error {
			return s.setAccountingPrivateData(phaseCtx, p.OriginalPrivateData)
		}},
		{"remove privacy DB records", s.removeAccountingPrivacyDB},
		{"remove privacy Linux users", s.removeAccountingPrivacyUsers},
	} {
		phaseCtx, cancel := context.WithTimeout(ctx, accountingPrivacyRolloutTimeout)
		err := phase.run(phaseCtx)
		cancel()
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", phase.name, err))
		}
	}
	err := errors.Join(failures...)
	if artifactErr := p.record(ctx, "root", "restore privacy configuration and remove fixtures", "", err); artifactErr != nil {
		err = errors.Join(err, artifactErr)
	}
	if err == nil {
		s.privacy = nil
	}
	return err
}

func (s *Accounting) cancelAccountingPrivacyJobs(ctx context.Context) error {
	var names []string
	for _, user := range s.privacy.Users[:3] {
		names = append(names, s.privacy.Prefix+"-"+user.Role)
	}
	output, err := s.privacyQuery(ctx, "root", "squeue -h --name="+framework.ShellQuote(strings.Join(names, ","))+" -o '%i'")
	if err != nil {
		return err
	}
	for _, id := range strings.Fields(output) {
		if _, err := strconv.ParseUint(id, 10, 64); err != nil {
			return fmt.Errorf("parse privacy cleanup JobID %q: %w", id, err)
		}
		if err := s.slurm.CancelJob(ctx, id, accountingCleanupTimeout); err != nil {
			return err
		}
	}
	return nil
}

func (s *Accounting) removeAccountingPrivacyDB(ctx context.Context) error {
	p := s.privacy
	for _, resource := range []struct {
		kind  string
		field string
		names []string
	}{{"user", "User", p.userNames()}, {"account", "Account", p.Accounts}} {
		filter := "name=" + framework.ShellQuote(strings.Join(resource.names, ","))
		query := "sacctmgr -nP show " + resource.kind + " where " + filter + " format=" + resource.field + "%64"
		output, err := s.privacyQuery(ctx, "root", query)
		if err != nil {
			return err
		}
		if strings.TrimSpace(output) == "" {
			continue
		}
		if _, err := s.privacyQuery(ctx, "root", "sacctmgr -i delete "+resource.kind+" where "+filter); err != nil {
			return err
		}
		output, err = s.privacyQuery(ctx, "root", query)
		if err != nil {
			return err
		}
		if err := validateAccountingPrivacyRows(output, 1, nil); err != nil {
			return err
		}
	}
	_, err := s.runtime.Controller().Run(ctx, "scontrol reconfigure")
	return err
}

func (s *Accounting) removeAccountingPrivacyUsers(ctx context.Context) error {
	var failures []error
	for _, user := range s.privacy.Users {
		quoted := framework.ShellQuote(user.Name)
		command := "if id " + quoted + " >/dev/null 2>&1; then deluser --remove-home " + quoted + "; fi"
		if _, err := s.privacyQuery(ctx, "root", command); err != nil {
			failures = append(failures, err)
			continue
		}
		command = "if getent group " + quoted + " >/dev/null; then delgroup " + quoted + "; fi"
		if _, err := s.privacyQuery(ctx, "root", command); err != nil {
			failures = append(failures, err)
		}
		if _, err := s.privacyQuery(ctx, "root", "test ! -e "+framework.ShellQuote(user.Home)); err != nil {
			failures = append(failures, fmt.Errorf("verify removal of privacy home %s: %w", user.Home, err))
		}
	}
	for _, database := range []string{"passwd", "group"} {
		command := "getent " + database + " " + strings.Join(s.privacy.userNames(), " ") + ` || [ "$?" -eq 2 ]`
		output, err := s.privacyQuery(ctx, "root", command)
		if err != nil {
			failures = append(failures, err)
		} else if strings.TrimSpace(output) != "" {
			failures = append(failures, fmt.Errorf("verify removal of privacy %s records, observed %s", database, output))
		}
	}
	return errors.Join(failures...)
}
