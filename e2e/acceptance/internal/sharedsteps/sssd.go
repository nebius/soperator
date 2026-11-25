package sharedsteps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	sssdTimeout       = 2 * time.Minute
	sssdLookupUser    = "nobody"
	sssdLookupCommand = "timeout 10s env SSS_NSS_USE_MEMCACHE=NO getent -s sss passwd " + sssdLookupUser
)

type sssdTarget struct {
	podName, container string
}

type SSSD struct {
	info     *framework.ClusterInfo
	runtime  framework.Runtime
	slurm    *framework.SlurmClient
	kubectl  *framework.KubectlClient
	selector *framework.WorkerSelector

	worker          framework.WorkerInfo
	targets         []sssdTarget
	sshOutput       string
	ownsReservation bool
}

func NewSSSD(info *framework.ClusterInfo, runtime framework.Runtime, slurm *framework.SlurmClient,
	kubectl *framework.KubectlClient, selector *framework.WorkerSelector) *SSSD {
	return &SSSD{info: info, runtime: runtime, slurm: slurm, kubectl: kubectl, selector: selector}
}

func (s *SSSD) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^SSSD is enabled without external configuration$`, s.defaultConfigurationIsEnabled)
	sc.Step(`^SSSD resolves an identity on login, controller, worker and enabled accounting$`, s.checkSSSD)
	sc.Step(`^the local user resolves consistently on login, controller and worker$`, s.checkUser)
	sc.Step(`^the local user connects from login to the worker over SSH$`, s.connect)
	sc.Step(`^the SSH connection reports the expected user and worker$`, s.checkConnection)
	sc.Step(`^a Slurm job submitted by that user succeeds$`, s.runJob)
}

func (s *SSSD) CleanupAndReset(ctx context.Context) {
	if s.ownsReservation {
		if err := framework.FinishWorkerReservation(ctx, s.runtime); err != nil {
			s.runtime.Logf("Cleanup: finish SSSD worker reservation: %v", err)
			framework.RecordWorkerCleanupError(ctx, err)
		}
	}
	s.worker = framework.WorkerInfo{}
	s.targets = nil
	s.sshOutput = ""
	s.ownsReservation = false
}

func (s *SSSD) defaultConfigurationIsEnabled(ctx context.Context) (context.Context, error) {
	var cluster kubeobjects.SlurmCluster
	if err := s.kubectl.GetJSON(ctx, &cluster, "get", "slurmcluster", s.info.SlurmClusterName,
		"-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
		return ctx, err
	}
	var nodeSets kubeobjects.NodeSetList
	if err := s.kubectl.GetJSON(ctx, &nodeSets, "get", "nodesets", "-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
		return ctx, err
	}
	if !defaultSSSDConfigured(cluster, nodeSets.Items) {
		s.runtime.Logf("SSSD: operator-default configuration is not enabled on login, controller and workers, skipping scenario")
		return ctx, godog.ErrSkip
	}
	if !framework.HasWorkerReservation(ctx) {
		ctx = framework.WithWorkerReservation(ctx, framework.NewReservationManager(), "default SSSD")
		s.ownsReservation = true
	}
	workers, err := s.selector.Acquire(ctx, framework.WorkerRequirements{Count: 1})
	if err != nil {
		return ctx, framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]
	var components = []struct{ role, container string }{{"login", loginSSHDContainer}, {"controller", "slurmctld"}}
	if cluster.Spec.SlurmNodes.Accounting.Enabled {
		components = append(components, struct{ role, container string }{"accounting", "accounting"})
	}
	for _, component := range components {
		pod, err := s.kubectl.ReadyWorkloadPodName(ctx, s.info.SlurmClusterName, component.role)
		if err != nil {
			return ctx, err
		}
		s.targets = append(s.targets, sssdTarget{pod, component.container})
	}
	pod, err := s.kubectl.WorkerPodForSlurmNode(ctx, s.worker.Name)
	if err != nil {
		return ctx, err
	}
	s.targets = append(s.targets, sssdTarget{pod.PodName, slurmdContainerName})
	return ctx, nil
}

func defaultSSSDConfigured(cluster kubeobjects.SlurmCluster, nodeSets []kubeobjects.NodeSet) bool {
	usesDefault := func(config kubeobjects.SSSDConfig) bool {
		return config.SSSD != nil && config.SSSDConfSecretRefName == "" && config.SSSDLdapCAConfigMapRefName == ""
	}
	if !usesDefault(cluster.Spec.SlurmNodes.Login.SSSDConfig) || !usesDefault(cluster.Spec.SlurmNodes.Controller) {
		return false
	}
	for _, nodeSet := range nodeSets {
		if nodeSet.Spec.ClusterName != "" && nodeSet.Spec.ClusterName != cluster.Metadata.Name {
			continue
		}
		if nodeSet.Spec.Replicas > 0 && !usesDefault(nodeSet.Spec.SSSDConfig) {
			return false
		}
	}
	return true
}

func (s *SSSD) checkSSSD(ctx context.Context) error {
	return s.runtime.WaitFor(ctx, "SSSD identity resolution", sssdTimeout, framework.DefaultPollInterval,
		func(ctx context.Context) (bool, error) {
			for _, target := range s.targets {
				output, err := s.runtime.Kubectl().Run(ctx, "exec", "-n", framework.SoperatorNamespace, target.podName,
					"-c", "sssd", "--", "getent", "-s", "files", "passwd", sssdLookupUser)
				if err != nil {
					return false, fmt.Errorf("read SSSD source identity on %s: %w", target.podName, err)
				}
				expected, err := sssdUserIdentity(output, sssdLookupUser)
				if err != nil {
					return false, err
				}
				var probes = []struct{ container, root string }{{"sssd", "/"}, {target.container, "/"}}
				if target.container == loginSSHDContainer || target.container == slurmdContainerName {
					probes = append(probes, struct{ container, root string }{target.container, "/mnt/jail"})
				}
				for _, probe := range probes {
					var args = []string{"exec", "-n", framework.SoperatorNamespace, target.podName, "-c", probe.container, "--"}
					if probe.root != "/" {
						args = append(args, "chroot", probe.root)
					}
					args = append(args, "sh", "-c", sssdLookupCommand)
					output, err := s.runtime.Kubectl().Run(ctx, args...)
					if err != nil {
						return false, fmt.Errorf("query SSSD in %s/%s root %s: %w", target.podName, probe.container, probe.root, err)
					}
					actual, err := sssdUserIdentity(output, sssdLookupUser)
					if err != nil || actual != expected {
						return false, fmt.Errorf("verify SSSD UID:GID in %s/%s root %s: expected %s, got %q, error=%v", target.podName, probe.container, probe.root, expected, output, err)
					}
				}
				s.runtime.Logf("SSSD: pod=%s user=%s UID:GID=%s", target.podName, sssdLookupUser, expected)
			}
			return true, nil
		})
}

func sssdUserIdentity(output, userName string) (string, error) {
	fields := strings.Split(strings.TrimSpace(output), ":")
	if len(fields) != 7 || fields[0] != userName || fields[2] == "" || fields[2] == "0" || fields[3] == "" {
		return "", fmt.Errorf("read non-root user %s identity from %q", userName, output)
	}
	return strings.Join(fields[2:4], ":"), nil
}

func (s *SSSD) checkUser(ctx context.Context) error {
	if err := prepareSSHTestUser(ctx, s.runtime, sshTestUserName, s.worker); err != nil {
		return err
	}
	var expected string
	for _, scope := range []framework.CommandScope{s.runtime.Jail(), s.runtime.Controller(), s.runtime.Worker(s.worker)} {
		output, err := scope.Run(ctx, "getent passwd "+framework.ShellQuote(sshTestUserName))
		if err != nil {
			return err
		}
		identity, err := sssdUserIdentity(output, sshTestUserName)
		if err != nil {
			return err
		}
		if expected != "" && identity != expected {
			return fmt.Errorf("verify local user %s UID:GID: expected %s, got %s", sshTestUserName, expected, identity)
		}
		expected = identity
	}
	return nil
}

func sssdIdentityProbe() string {
	return sssdLookupCommand + ` >/dev/null && ` + jobUserProbe() + ` && printf 'JOB_HOST=%s\n' "$(hostname)"`
}

func (s *SSSD) connect(ctx context.Context) error {
	output, err := runInternalSSHAsTestUser(ctx, s.runtime, s.worker.Name, internalSSHTimeout, sssdIdentityProbe())
	s.sshOutput = output
	return err
}

func (s *SSSD) checkConnection() error {
	return s.checkWorkloadIdentity(s.sshOutput)
}

func (s *SSSD) checkWorkloadIdentity(output string) error {
	if err := validateJobUser(output, sshTestUserName); err != nil {
		return err
	}
	if host := parseKeyValueLine(output, "JOB_HOST"); host != s.worker.Name {
		return fmt.Errorf("verify SSSD workload host: expected %s, got %q", s.worker.Name, host)
	}
	return nil
}

func (s *SSSD) runJob(ctx context.Context) error {
	jobCtx, cancel := context.WithTimeout(ctx, sssdTimeout)
	defer cancel()
	output, err := s.slurm.RunSlurm(jobCtx, "srun", framework.SlurmCommandOptions{
		Nodelist: []string{s.worker.Name}, RunAsUser: sshTestUserName, TimeLimit: time.Minute,
		Args: []string{"--ntasks=1", "--cpus-per-task=1"},
	}, "bash", "-c", sssdIdentityProbe())
	if err != nil {
		return err
	}
	return s.checkWorkloadIdentity(output)
}
