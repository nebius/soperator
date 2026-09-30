package sharedsteps

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	loginCPUFirstUser  = "soperatorcpua"
	loginCPUSecondUser = "soperatorcpub"

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
	balancedWork   loginCPUWork
	unbalancedWork loginCPUWork
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
	s.capacity = 0
	s.balancedWork = loginCPUWork{}
	s.unbalancedWork = loginCPUWork{}
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

	for _, userName := range []string{loginCPUFirstUser, loginCPUSecondUser} {
		if err := ensureSSHTestUser(ctx, s.runtime, userName); err != nil {
			return err
		}
		if err := s.verifyUserCanSSH(ctx, userName); err != nil {
			return err
		}
	}

	capacity, err := s.loginCPUCapacity(ctx)
	if err != nil {
		return err
	}
	s.capacity = capacity
	s.runtime.Logf("login CPU capacity: %d", capacity)
	return nil
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

func (s *LoginUserIsolation) verifyUserCanSSH(ctx context.Context, userName string) error {
	command := fmt.Sprintf(
		"su - %s -c %s",
		framework.ShellQuote(userName),
		framework.ShellQuote(loginCPUSSHCommand("true")),
	)
	if _, err := s.runtime.Jail().Run(ctx, command); err != nil {
		return fmt.Errorf("verify login CPU isolation SSH connectivity for %s: %w", userName, err)
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
	_, err := s.runtime.Jail().Run(ctx, command)
	return err
}

func (s *LoginUserIsolation) loginCPUCapacity(ctx context.Context) (int, error) {
	output, err := s.runtime.Jail().Run(ctx, "nproc")
	if err != nil {
		return 0, fmt.Errorf("read effective login CPU capacity: %w", err)
	}
	value := strings.TrimSpace(output)
	capacity, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse effective login CPU capacity %q: %w", value, err)
	}
	if capacity <= 0 {
		return 0, fmt.Errorf("parse effective login CPU capacity %q: must be positive", value)
	}
	return capacity, nil
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

func (s *LoginUserIsolation) startCPUWorkload(
	ctx context.Context,
	userName string,
	burners int,
	results chan<- loginCPUWorkloadResult,
) {
	go func() {
		output, err := s.runtime.Jail().Run(ctx, loginCPUWorkloadCommand(userName, burners))
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
pids=()
for ((i = 0; i < %d; i++)); do
    bash -c %s >"${workdir}/${i}" &
    pids+=("$!")
done
worker_failed=0
for i in "${!pids[@]}"; do
    if ! wait "${pids[$i]}"; then
        printf 'login CPU worker %%d failed\n' "${i}" >&2
        worker_failed=1
    fi
done
if ((worker_failed)); then
    exit 1
fi
awk '{total += $1} END {printf "SOPERATOR_E2E_LOGIN_CPU_WORK %%.0f\n", total}' "${workdir}"/*
`, burners, framework.ShellQuote(burnScript))
	return fmt.Sprintf(
		"su - %s -c %s",
		framework.ShellQuote(userName),
		framework.ShellQuote(loginCPUSSHCommand(framework.BashLC(remoteScript))),
	)
}

func loginCPUSSHCommand(command string) string {
	return fmt.Sprintf(
		"timeout %.0f ssh -i ~/.ssh/id_ecdsa -o IdentitiesOnly=yes -o BatchMode=yes -o LogLevel=ERROR -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null localhost %s",
		loginCPUCommandTimeout.Seconds(),
		framework.ShellQuote(command),
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
