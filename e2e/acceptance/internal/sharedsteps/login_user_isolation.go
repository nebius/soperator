package sharedsteps

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	loginCPUFirstUser  = "soperatorcpua"
	loginCPUSecondUser = "soperatorcpub"

	loginCPUKeyName    = "soperator_e2e_login_cpu"
	loginCPUKeyComment = "soperator-e2e-login-cpu"
	loginCPUBurnMarker = "soperator-e2e-login-cpu-burn"

	loginCPUWorkloadDuration = 8 * time.Second
	loginCPUCommandTimeout   = 20 * time.Second

	loginCPUMinShare            = 0.35
	loginCPUMaxShare            = 0.65
	loginCPUMaxWorkRelativeDiff = 0.20
)

type loginCPUWork struct {
	first  uint64
	second uint64
}

type loginCPUWorkloadResult struct {
	userName string
	work     uint64
	err      error
}

type LoginUserIsolation struct {
	info    *framework.ClusterInfo
	runtime framework.Runtime

	capacity       int
	loginPodName   string
	balancedWork   loginCPUWork
	unbalancedWork loginCPUWork
	identitySet    bool
	workloadActive bool
}

func NewLoginUserIsolation(info *framework.ClusterInfo, runtime framework.Runtime) *LoginUserIsolation {
	return &LoginUserIsolation{info: info, runtime: runtime}
}

func (s *LoginUserIsolation) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^two regular users can SSH to the login node for CPU isolation testing$`, s.twoRegularUsersCanSSHToTheLoginNode)
	sc.Step(`^both users run workloads at twice the login CPU capacity$`, s.bothUsersRunAtTwiceCapacity)
	sc.Step(`^the first user runs at login CPU capacity while the second runs at four times capacity$`, s.usersRunAtDifferentLoads)
	sc.Step(`^both users complete similar amounts of work in both cases$`, s.bothUsersCompleteSimilarAmountsOfWork)
	sc.Step(`^the first user's amount of completed work remains stable$`, s.firstUsersAmountOfWorkRemainsStable)
}

func (s *LoginUserIsolation) CleanupAndReset(ctx context.Context) {
	if s.workloadActive {
		if err := s.stopCPUWorkloads(ctx); err != nil {
			s.runtime.Logf("cleanup: stop login CPU isolation workloads: %v", err)
		}
	}
	if s.identitySet {
		if err := s.removeSSHIdentities(ctx); err != nil {
			s.runtime.Logf("cleanup: remove login CPU isolation SSH identities: %v", err)
		}
	}

	s.capacity = 0
	s.loginPodName = ""
	s.balancedWork = loginCPUWork{}
	s.unbalancedWork = loginCPUWork{}
	s.identitySet = false
	s.workloadActive = false
}

func (s *LoginUserIsolation) twoRegularUsersCanSSHToTheLoginNode(ctx context.Context) error {
	enabled, err := s.loginUserIsolationIsEnabled(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		s.runtime.Logf("acceptance: login user isolation is disabled, skipping scenario")
		return godog.ErrSkip
	}

	loginPodName, err := s.discoverLoginPod(ctx)
	if err != nil {
		return err
	}
	s.loginPodName = loginPodName

	for _, userName := range []string{loginCPUFirstUser, loginCPUSecondUser} {
		if err := s.ensureSSHTestUser(ctx, userName); err != nil {
			return err
		}
	}

	s.identitySet = true
	if err := s.prepareSSHIdentities(ctx); err != nil {
		return err
	}

	capacity, err := s.loginCPUCapacity(ctx)
	if err != nil {
		return err
	}
	s.capacity = capacity
	s.runtime.Logf("login CPU capacity: %d", capacity)
	return nil
}

func (s *LoginUserIsolation) ensureSSHTestUser(ctx context.Context, userName string) error {
	quotedUserName := framework.ShellQuote(userName)
	command := fmt.Sprintf(
		"id %s >/dev/null 2>&1 || printf '\n' | createuser --without-external-ssh %s",
		quotedUserName,
		quotedUserName,
	)
	if _, err := s.runInLoginJail(ctx, command); err != nil {
		return fmt.Errorf("create SSH test user %s: %w", userName, err)
	}
	return nil
}

func (s *LoginUserIsolation) discoverLoginPod(ctx context.Context) (string, error) {
	selector := fmt.Sprintf(
		"app.kubernetes.io/component=login,app.kubernetes.io/instance=%s,apps.kubernetes.io/pod-index=0",
		s.info.SlurmClusterName,
	)
	output, err := s.runtime.Kubectl().RunWithDefaultRetry(
		ctx,
		"get",
		"pods",
		"-n",
		framework.SoperatorNamespace,
		"-l",
		selector,
		"-o",
		`jsonpath={range .items[*]}{.metadata.name}{"\n"}{end}`,
	)
	if err != nil {
		return "", fmt.Errorf("discover login pod: %w", err)
	}

	podNames := strings.Fields(output)
	if len(podNames) == 0 {
		return "", fmt.Errorf("discover login pod: no index-0 pod found")
	}
	return podNames[0], nil
}

func (s *LoginUserIsolation) loginUserIsolationIsEnabled(ctx context.Context) (bool, error) {
	output, err := s.runtime.Kubectl().RunWithDefaultRetry(
		ctx,
		"get",
		"slurmcluster",
		s.info.SlurmClusterName,
		"-n",
		framework.SoperatorNamespace,
		"-o",
		"jsonpath={.spec.slurmNodes.login.userIsolation.enabled}",
	)
	if err != nil {
		return false, fmt.Errorf("read login user isolation configuration: %w", err)
	}
	return strings.TrimSpace(output) == "true", nil
}

func (s *LoginUserIsolation) bothUsersRunAtTwiceCapacity(ctx context.Context) error {
	if s.capacity < 1 {
		return fmt.Errorf("run balanced login CPU load: CPU capacity is not initialized")
	}

	work, err := s.runCPUContention(ctx, 2*s.capacity, 2*s.capacity)
	if err != nil {
		return fmt.Errorf("run balanced login CPU load: %w", err)
	}
	s.balancedWork = work
	return nil
}

func (s *LoginUserIsolation) usersRunAtDifferentLoads(ctx context.Context) error {
	if s.capacity < 1 {
		return fmt.Errorf("run unbalanced login CPU load: CPU capacity is not initialized")
	}

	work, err := s.runCPUContention(ctx, s.capacity, 4*s.capacity)
	if err != nil {
		return fmt.Errorf("run unbalanced login CPU load: %w", err)
	}
	s.unbalancedWork = work
	return nil
}

func (s *LoginUserIsolation) bothUsersCompleteSimilarAmountsOfWork() error {
	phases := []struct {
		name string
		work loginCPUWork
	}{
		{name: "balanced", work: s.balancedWork},
		{name: "unbalanced", work: s.unbalancedWork},
	}
	for _, phase := range phases {
		share, err := firstCPUWorkShare(phase.work)
		if err != nil {
			return fmt.Errorf("validate %s login CPU shares: %w", phase.name, err)
		}
		s.runtime.Logf(
			"%s login CPU work: first=%d second=%d first_share=%.3f",
			phase.name,
			phase.work.first,
			phase.work.second,
			share,
		)
		if share < loginCPUMinShare || share > loginCPUMaxShare {
			return fmt.Errorf(
				"validate %s login CPU shares: first user share %.3f is outside [%.2f, %.2f]",
				phase.name,
				share,
				loginCPUMinShare,
				loginCPUMaxShare,
			)
		}
	}
	return nil
}

func (s *LoginUserIsolation) firstUsersAmountOfWorkRemainsStable() error {
	difference, err := relativeDifference(s.balancedWork.first, s.unbalancedWork.first)
	if err != nil {
		return fmt.Errorf("compare first user's login CPU work: %w", err)
	}
	s.runtime.Logf("first user login CPU work relative difference: %.3f", difference)
	if difference > loginCPUMaxWorkRelativeDiff {
		return fmt.Errorf(
			"compare first user's login CPU work: relative difference %.3f exceeds %.2f",
			difference,
			loginCPUMaxWorkRelativeDiff,
		)
	}
	return nil
}

func (s *LoginUserIsolation) prepareSSHIdentities(ctx context.Context) error {
	for _, userName := range []string{loginCPUFirstUser, loginCPUSecondUser} {
		setupIdentity := fmt.Sprintf(`
set -euo pipefail
install -d -m 0700 "${HOME}/.ssh"
key="${HOME}/.ssh/%s"
authorized_keys="${HOME}/.ssh/authorized_keys"
rm -f "${key}" "${key}.pub"
touch "${authorized_keys}"
sed -i '\# %s$#d' "${authorized_keys}"
ssh-keygen -q -t ecdsa -N '' -C %s -f "${key}"
cat "${key}.pub" >> "${authorized_keys}"
chmod 0600 "${authorized_keys}"
`, loginCPUKeyName, loginCPUKeyComment, framework.ShellQuote(loginCPUKeyComment))
		command := fmt.Sprintf(
			"su - %s -c %s",
			framework.ShellQuote(userName),
			framework.ShellQuote(framework.BashLC(setupIdentity)),
		)
		if _, err := s.runInLoginJail(ctx, command); err != nil {
			return fmt.Errorf("prepare login CPU isolation SSH identity for %s: %w", userName, err)
		}
	}
	return nil
}

func (s *LoginUserIsolation) removeSSHIdentities(ctx context.Context) error {
	var cleanupErrors []string
	for _, userName := range []string{loginCPUFirstUser, loginCPUSecondUser} {
		cleanupIdentity := fmt.Sprintf(`
key="${HOME}/.ssh/%s"
authorized_keys="${HOME}/.ssh/authorized_keys"
rm -f "${key}" "${key}.pub"
if [ -f "${authorized_keys}" ]; then
    sed -i '\# %s$#d' "${authorized_keys}"
fi
`, loginCPUKeyName, loginCPUKeyComment)
		command := fmt.Sprintf(
			"if id %s >/dev/null 2>&1; then su - %s -c %s; fi",
			framework.ShellQuote(userName),
			framework.ShellQuote(userName),
			framework.ShellQuote(framework.BashLC(cleanupIdentity)),
		)
		if _, err := s.runInLoginJail(ctx, command); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("%s: %v", userName, err))
		}
	}
	if len(cleanupErrors) > 0 {
		return fmt.Errorf("remove SSH identities: %s", strings.Join(cleanupErrors, "; "))
	}
	return nil
}

func (s *LoginUserIsolation) stopCPUWorkloads(ctx context.Context) error {
	command := fmt.Sprintf(
		"pkill -u %s -f %s 2>/dev/null || true; pkill -u %s -f %s 2>/dev/null || true",
		framework.ShellQuote(loginCPUFirstUser),
		framework.ShellQuote(loginCPUBurnMarker),
		framework.ShellQuote(loginCPUSecondUser),
		framework.ShellQuote(loginCPUBurnMarker),
	)
	_, err := s.runInLoginJail(ctx, command)
	return err
}

func (s *LoginUserIsolation) loginCPUCapacity(ctx context.Context) (int, error) {
	output, err := s.runtime.Kubectl().RunWithDefaultRetry(
		ctx,
		"get",
		"slurmcluster",
		s.info.SlurmClusterName,
		"-n",
		framework.SoperatorNamespace,
		"-o",
		"jsonpath={.spec.slurmNodes.login.sshd.resources.cpu}",
	)
	if err != nil {
		return 0, fmt.Errorf("read configured login CPU capacity: %w", err)
	}
	value := strings.TrimSpace(output)
	quantity, err := resource.ParseQuantity(value)
	if err != nil {
		return 0, fmt.Errorf("parse configured login CPU capacity %q: %w", value, err)
	}
	milliCPU := quantity.MilliValue()
	if milliCPU <= 0 {
		return 0, fmt.Errorf("parse configured login CPU capacity %q: must be positive", value)
	}
	return int((milliCPU + 999) / 1000), nil
}

func (s *LoginUserIsolation) runCPUContention(ctx context.Context, firstBurners, secondBurners int) (loginCPUWork, error) {
	workloadCtx, cancelWorkloads := context.WithCancel(ctx)
	defer cancelWorkloads()
	results := make(chan loginCPUWorkloadResult, 2)

	s.workloadActive = true
	s.startCPUWorkload(workloadCtx, loginCPUFirstUser, firstBurners, results)
	s.startCPUWorkload(workloadCtx, loginCPUSecondUser, secondBurners, results)

	work, err := waitForLoginCPUWorkloads(ctx, results)
	if err != nil {
		return loginCPUWork{}, err
	}
	s.workloadActive = false
	return work, nil
}

func (s *LoginUserIsolation) runInLoginContainer(ctx context.Context, script string) (string, error) {
	return s.runInLoginPod(ctx, "bash", "-lc", script)
}

func (s *LoginUserIsolation) runInLoginJail(ctx context.Context, command string) (string, error) {
	return s.runInLoginPod(ctx, "chroot", "/mnt/jail", "bash", "-lc", command)
}

func (s *LoginUserIsolation) runInLoginPod(ctx context.Context, command ...string) (string, error) {
	args := []string{
		"exec",
		"-n",
		framework.SoperatorNamespace,
		s.loginPodName,
		"--",
	}
	args = append(args, command...)
	return s.runtime.Kubectl().Run(ctx, args...)
}

func (s *LoginUserIsolation) startCPUWorkload(
	ctx context.Context,
	userName string,
	burners int,
	results chan<- loginCPUWorkloadResult,
) {
	go func() {
		output, err := s.runInLoginContainer(ctx, loginCPUWorkloadCommand(userName, burners))
		if err != nil {
			err = fmt.Errorf("run login CPU workload for %s: %w", userName, err)
		}
		var work uint64
		if err == nil {
			work, err = parseLoginCPUWork(output)
			if err != nil {
				err = fmt.Errorf("read completed login CPU work for %s: %w", userName, err)
			}
		}
		results <- loginCPUWorkloadResult{userName: userName, work: work, err: err}
	}()
}

func waitForLoginCPUWorkloads(ctx context.Context, results <-chan loginCPUWorkloadResult) (loginCPUWork, error) {
	var work loginCPUWork
	var workloadErrors []error
	for range 2 {
		select {
		case <-ctx.Done():
			return loginCPUWork{}, ctx.Err()
		case result := <-results:
			if result.err != nil {
				workloadErrors = append(workloadErrors, result.err)
				continue
			}
			switch result.userName {
			case loginCPUFirstUser:
				work.first = result.work
			case loginCPUSecondUser:
				work.second = result.work
			default:
				workloadErrors = append(workloadErrors, fmt.Errorf("read completed login CPU work for unknown user %q", result.userName))
			}
		}
	}
	if err := errors.Join(workloadErrors...); err != nil {
		return loginCPUWork{}, err
	}
	return work, nil
}

func loginCPUWorkloadCommand(userName string, burners int) string {
	burnScript := fmt.Sprintf(`
marker=%s
count=0
deadline=$((SECONDS + %.0f))
while ((SECONDS < deadline)); do
    ((count += 1))
done
printf '%%s\n' "${count}"
`, framework.ShellQuote(loginCPUBurnMarker), loginCPUWorkloadDuration.Seconds())
	remoteScript := fmt.Sprintf(`
set -euo pipefail
workdir="$(mktemp -d /tmp/soperator-e2e-login-cpu.XXXXXX)"
trap 'rm -r -- "${workdir}"' EXIT
for ((i = 0; i < %d; i++)); do
    bash -c %s >"${workdir}/${i}" &
done
wait
awk '{total += $1} END {printf "SOPERATOR_E2E_LOGIN_CPU_WORK %%.0f\n", total}' "${workdir}"/*
`, burners, framework.ShellQuote(burnScript))
	sshCommand := fmt.Sprintf(
		"timeout %.0f ssh -i ~/.ssh/%s -o IdentitiesOnly=yes -o BatchMode=yes -o LogLevel=ERROR -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null localhost %s",
		loginCPUCommandTimeout.Seconds(),
		loginCPUKeyName,
		framework.ShellQuote(framework.BashLC(remoteScript)),
	)
	return fmt.Sprintf(
		"chroot /mnt/jail su - %s -c %s",
		framework.ShellQuote(userName),
		framework.ShellQuote(sshCommand),
	)
}

func parseLoginCPUWork(output string) (uint64, error) {
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "SOPERATOR_E2E_LOGIN_CPU_WORK" {
			continue
		}
		if len(fields) != 2 {
			return 0, fmt.Errorf("parse login CPU work line %q", line)
		}
		work, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse login CPU work %q: %w", fields[1], err)
		}
		return work, nil
	}
	return 0, fmt.Errorf("parse login CPU work: marker line not found in %q", strings.TrimSpace(output))
}

func firstCPUWorkShare(work loginCPUWork) (float64, error) {
	total := work.first + work.second
	if total == 0 {
		return 0, fmt.Errorf("combined CPU work is zero")
	}
	return float64(work.first) / float64(total), nil
}

func relativeDifference(first, second uint64) (float64, error) {
	maximum := max(first, second)
	if maximum == 0 {
		return 0, fmt.Errorf("both CPU work values are zero")
	}
	minimum := min(first, second)
	return float64(maximum-minimum) / float64(maximum), nil
}
