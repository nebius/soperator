package framework

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLostSubmissionResponseIsCleanedByReservation(t *testing.T) {
	for _, output := range []string{"", "17\n", "1"} {
		t.Run("output="+output, func(t *testing.T) {
			runtime := newReservationRuntime()
			ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
			runtime.submitErr = errors.New("unexpected EOF")
			runtime.submitOutput = output
			job, err := NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{JobName: "lost", Wrap: "true", Nodelist: []string{"worker-0"}})
			require.Error(t, err)
			if output == "17\n" {
				assert.Equal(t, "17", job.ID)
			} else {
				assert.Empty(t, job.ID)
			}
			require.NoError(t, FinishWorkerReservation(ctx, runtime))
			assert.Equal(t, 1, runtime.authority.cancelled)
			assert.Empty(t, runtime.authority.leases)
		})
	}
}

func TestDefinitePreparationFailureDoesNotPoisonLease(t *testing.T) {
	runtime := newReservationRuntime()
	ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
	runtime.prepErr = errors.New("sudo: user does not exist")
	_, err := NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{JobName: "prepare", Wrap: "true", RunAsUser: "test-user"})
	require.ErrorContains(t, err, "prepare sbatch")
	assert.Empty(t, runtime.authority.jobs)
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
	assert.Empty(t, runtime.authority.leases)
}

func TestBoundCommandsPreserveSelectedWorkersAndLoginBinding(t *testing.T) {
	runtime := newReservationRuntime()
	ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0", "worker-1")
	slurm := NewSlurmClient(runtime)
	name, _ := WorkerReservationName(ctx)
	for _, command := range []string{"srun", "salloc"} {
		_, err := slurm.RunSlurm(ctx, command, SlurmCommandOptions{RunAsUser: "test-user", Nodelist: []string{"worker-0"}, Args: []string{"--mem=128M", "--nodelist=worker-1"}}, "bash", "-c", "echo hello; true")
		require.NoError(t, err)
		actual := runtime.authority.calls[len(runtime.authority.calls)-1]
		assert.Contains(t, actual, "'sudo' '-iu' 'test-user' '--' 'env'")
		assert.Contains(t, actual, "'--reservation="+name+"'")
		assert.Greater(t, strings.LastIndex(actual, "--nodelist=worker-0"), strings.Index(actual, "--nodelist=worker-1"))
		assert.Contains(t, actual, "'--' 'bash' '-c' 'echo hello; true'")
	}
	_, err := slurm.RunReservationUtility(ctx, "test-user", "utility", "arg with spaces")
	require.NoError(t, err)
	actual := runtime.authority.calls[len(runtime.authority.calls)-1]
	assert.Contains(t, actual, "'SBATCH_RESERVATION="+name+"'")
	assert.Contains(t, actual, "'SALLOC_RESERVATION="+name+"'")
	assert.Contains(t, actual, "'SLURM_RESERVATION="+name+"'")
	count := 0
	for _, command := range runtime.authority.calls {
		if strings.Contains(command, "Users+=") {
			count++
		}
	}
	assert.Equal(t, 1, count)
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
}

func TestBoundSubmissionValidationIsTargeted(t *testing.T) {
	runtime := newReservationRuntime()
	ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
	for _, args := range [][]string{{"--reservation=other"}, {"--nodefile=/tmp/nodes"}, {"-F/tmp/nodes"}, {":"}, {"--"}} {
		_, err := NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{JobName: "invalid", Wrap: "true", Nodelist: []string{"worker-0"}, Args: args})
		require.Error(t, err)
	}
	_, err := NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{JobName: "legacy", Wrap: "true", ExtraFlags: []string{"--mem=128M --exclusive"}})
	require.ErrorContains(t, err, "migrate legacy ExtraFlags")
	_, err = NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{JobName: "outside", Wrap: "true", Nodelist: []string{"worker-1"}})
	require.ErrorContains(t, err, "outside")
	_, err = NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{JobName: "argv", Wrap: "true", Nodelist: []string{"worker-0"}, Args: []string{"--exclusive", "--mem=128M"}})
	require.NoError(t, err)
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
}

func TestBoundStepsMustBelongToLease(t *testing.T) {
	runtime := newReservationRuntime()
	ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
	slurm := NewSlurmClient(runtime)
	_, err := slurm.RunSlurm(ctx, "srun", SlurmCommandOptions{JobID: "foreign"}, "true")
	require.ErrorContains(t, err, "outside")
	job, err := slurm.SubmitBatch(ctx, SbatchOptions{JobName: "own", Wrap: "sleep 100"})
	require.NoError(t, err)
	_, err = slurm.RunSlurm(ctx, "srun", SlurmCommandOptions{JobID: job.ID}, "hostname")
	require.NoError(t, err)
	_, err = slurm.RunSlurm(ctx, "srun", SlurmCommandOptions{Args: []string{"--jobid=foreign"}}, "true")
	require.ErrorContains(t, err, "use JobID")
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
}

func TestLimitsAreConfigurableWithoutScenarioDeclarations(t *testing.T) {
	runtime := newReservationRuntime()
	ctx := WithReservationOptions(t.Context(), ReservationOptions{LeaseDuration: 12 * time.Hour, JobTimeLimit: 3 * time.Hour})
	ctx = WithWorkerReservation(ctx, NewReservationManager(), "long profile")
	_, err := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator").Acquire(ctx, WorkerRequirements{Count: 1})
	require.NoError(t, err)
	_, err = NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{JobName: "long", Wrap: "true", TimeLimit: 4 * time.Hour})
	require.NoError(t, err)
	assert.Contains(t, runtime.authority.calls[len(runtime.authority.calls)-1], "--time=4:00:00")
	_, err = NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{JobName: "too long", Wrap: "true", TimeLimit: 12 * time.Hour})
	require.ErrorContains(t, err, "exceeds usable")
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
	assert.Equal(t, 2*time.Hour, WorkerAcquireTimeout(context.Background()))
}

// A definite rejection has no job to discover and must not mark restoration unsafe.
type rejectingReservationRuntime struct{ *reservationRuntime }

func (r rejectingReservationRuntime) Jail() CommandScope {
	base := r.reservationRuntime.Jail()
	return NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if strings.Contains(command, "sbatch ") {
			return "", kubectlCommandFailure("sbatch: error: Batch job submission failed: Invalid partition")
		}
		return base.Run(ctx, command)
	})
}
func TestDefiniteSubmissionRejectionDoesNotPoisonLease(t *testing.T) {
	runtime := newReservationRuntime()
	ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
	_, err := NewSlurmClient(rejectingReservationRuntime{runtime}).SubmitBatch(ctx, SbatchOptions{JobName: "rejected", Wrap: "true"})
	require.ErrorContains(t, err, "Invalid partition")
	assert.NotContains(t, err.Error(), "outcome uncertain")
	assert.Empty(t, runtime.authority.jobs)
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
	assert.Empty(t, runtime.authority.leases)
}

func TestAllocationArgsCannotTurnBindingIntoPayload(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		t.Run(fmt.Sprint(scoped), func(t *testing.T) {
			runtime := newReservationRuntime()
			ctx := t.Context()
			if scoped {
				ctx = acquireNative(t, runtime, NewReservationManager(), "worker-0", "worker-1")
			}
			client := NewSlurmClient(runtime)
			for _, args := range [][]string{{"--nodelist=worker-1", "hostname"}, {"--mem", "128M"}, {"--"}, {":"}} {
				_, err := client.RunSlurm(ctx, "srun", SlurmCommandOptions{Nodelist: []string{"worker-0"}, Args: args}, "true")
				require.ErrorContains(t, err, "self-contained")
				_, err = client.SubmitBatch(ctx, SbatchOptions{JobName: "boundary", Wrap: "true", Nodelist: []string{"worker-0"}, Args: args})
				require.ErrorContains(t, err, "self-contained")
			}
			if scoped {
				require.NoError(t, FinishWorkerReservation(ctx, runtime))
			}
		})
	}
}

func TestBoundCommandsRejectNoAllocationMode(t *testing.T) {
	for _, flag := range []string{"--no-allocate", "--no-all", "-Z", "-vZ"} {
		t.Run(flag, func(t *testing.T) {
			runtime := newReservationRuntime()
			ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
			client := NewSlurmClient(runtime)
			before := len(runtime.authority.calls)
			_, err := client.RunSlurm(ctx, "srun", SlurmCommandOptions{Args: []string{flag, "--nodelist=unreserved-worker"}}, "true")
			require.ErrorContains(t, err, "no-allocate")
			assert.Len(t, runtime.authority.calls, before, "reject before executing or granting access")
			require.NoError(t, FinishWorkerReservation(ctx, runtime))
		})
	}
}

func TestBoundCommandsEnforceSelectedNodeCount(t *testing.T) {
	for _, command := range []string{"srun", "salloc"} {
		t.Run(command, func(t *testing.T) {
			runtime := newReservationRuntime()
			ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0", "worker-1")
			client := NewSlurmClient(runtime)
			for _, flag := range []string{"--nodes=2", "-N2", "--nodes=1-2"} {
				_, err := client.RunSlurm(ctx, command, SlurmCommandOptions{Nodelist: []string{"worker-0"}, Args: []string{flag}}, "hostname")
				require.NoError(t, err)
				actual := runtime.authority.calls[len(runtime.authority.calls)-1]
				require.Contains(t, actual, "'--nodes=1'")
				assert.Greater(t, strings.LastIndex(actual, "--nodes=1'"), strings.Index(actual, flag))
			}
			for _, options := range []SlurmCommandOptions{
				{Nodelist: []string{"worker-0"}, Nodes: 2},
				{Nodelist: []string{"worker-0", "worker-0"}},
				{Nodes: 3},
			} {
				before := len(runtime.authority.calls)
				_, err := client.RunSlurm(ctx, command, options, "hostname")
				require.Error(t, err)
				assert.Len(t, runtime.authority.calls, before)
			}
			require.NoError(t, FinishWorkerReservation(ctx, runtime))
		})
	}
}

func TestNoAllocationGuardDoesNotChangeUnscopedOrPayloadArguments(t *testing.T) {
	runtime := newReservationRuntime()
	client := NewSlurmClient(runtime)
	_, err := client.RunSlurm(t.Context(), "srun", SlurmCommandOptions{Args: []string{"-Z", "--nodelist=worker-1"}}, "true")
	require.NoError(t, err)
	ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
	_, err = client.RunSlurm(ctx, "srun", SlurmCommandOptions{Args: []string{"-vv", "--job-name=containsZ"}}, "printf", "%s", "-Z")
	require.NoError(t, err)
	assert.Contains(t, runtime.authority.calls[len(runtime.authority.calls)-1], "'--' 'printf' '%s' '-Z'")
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
}
