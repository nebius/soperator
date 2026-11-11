package framework

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReservedBatchJobsCarryLeaseAndGrantNormalUser(t *testing.T) {
	backend := newFakeReservations()
	ctx := WithWorkerReservation(t.Context(), NewWorkerPool(), t.Name())
	r := workerReservation(ctx)
	_, err := r.acquireAny(ctx, backend, fixedChoices(workerChoice{count: 1, candidates: reservationWorkers("cpu0")}))
	require.NoError(t, err)
	runtime := &submitBatchRuntime{}
	_, err = NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{
		JobName: "owned", Wrap: "hostname", Nodelist: []string{"cpu0"}, RunAsUser: "test-user",
		ExtraFlags: []string{"--time=5:00", "--reservation=wrong", "--comment=$(touch /tmp/unsafe)"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"test-user"}, backend.users)
	assert.Contains(t, runtime.jailCommand, "sudo -iu 'test-user' -- sbatch")
	assert.Contains(t, runtime.jailCommand, "--time=02:00:00")
	assert.Contains(t, runtime.jailCommand, "'--time=5:00'")
	assert.Contains(t, runtime.jailCommand, "'--comment=$(touch /tmp/unsafe)'")
	assert.Greater(t, strings.Index(runtime.jailCommand, "--reservation="+r.id), strings.Index(runtime.jailCommand, "--reservation=wrong"))
	for _, flags := range [][]string{{":"}, {"--"}} {
		_, err = NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{JobName: "boundary", Wrap: "true", Nodelist: []string{"cpu0"}, ExtraFlags: flags})
		require.ErrorContains(t, err, "option boundary")
	}
	_, err = NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{JobName: "outside", Wrap: "true", Nodelist: []string{"cpu1"}})
	require.ErrorContains(t, err, "outside the scenario reservation")
}

func TestRawSlurmClientsGetContextLocalReservationAcrossRetryMethods(t *testing.T) {
	runtime := &submitBatchRuntime{}
	wrapped := WithWorkerReservationRuntime(runtime)
	var names []string
	for _, name := range []string{"first", "second"} {
		ctx := WithWorkerReservation(t.Context(), NewWorkerPool(), name)
		r := workerReservation(ctx)
		r.backend = newFakeReservations()
		r.workers = reservationWorkers("cpu0")
		names = append(names, r.id)
		for _, run := range []func(context.Context, string) (string, error){
			wrapped.Jail().Run,
			func(ctx context.Context, command string) (string, error) {
				return wrapped.Jail().RunWithRetry(ctx, command, 1, time.Millisecond)
			},
			wrapped.Jail().RunWithDefaultRetry,
		} {
			_, err := run(ctx, "srun hostname")
			require.NoError(t, err)
			for _, env := range []string{"SBATCH_RESERVATION", "SLURM_RESERVATION", "SALLOC_RESERVATION"} {
				assert.Contains(t, runtime.jailCommand, env+"='"+r.id+"'")
			}
			assert.True(t, strings.HasSuffix(runtime.jailCommand, "; srun hostname"))
		}
	}
	assert.NotEqual(t, names[0], names[1])
	_, err := wrapped.Jail().Run(t.Context(), "srun hostname")
	require.NoError(t, err)
	assert.Equal(t, "srun hostname", runtime.jailCommand)
}

func TestSlurmReservationAdapterUsesStaticWholeNodesAndConfirmsDeletion(t *testing.T) {
	runtime := workerReadinessFixture()
	client := NewSlurmClient(runtime)
	require.NoError(t, client.createReservation(t.Context(), "e2e-test", []string{"worker-0", "worker-1"}))
	leases, err := client.reservations(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"worker-0", "worker-1"}, leases["e2e-test"])
	command := runtime.native.commands[0]
	assert.Contains(t, command, "Duration=08:00:00 Users=root Flags=STATIC_ALLOC Nodes='worker-0,worker-1'")
	for _, flag := range []string{"REPLACE", "OVERLAP", "IGNORE_JOBS", "MAINT", "FLEX"} {
		assert.NotContains(t, command, flag)
	}
	require.NoError(t, client.addReservationUser(t.Context(), "e2e-test", "normal-user"))
	assert.Contains(t, runtime.native.commands, "scontrol update ReservationName='e2e-test' Users+='normal-user'")
	require.ErrorContains(t, client.addReservationUser(t.Context(), "e2e-test", "root,another"), "invalid user")
	require.NoError(t, client.deleteReservation(t.Context(), "e2e-test"))
	assert.Empty(t, runtime.native.leases)
	require.NoError(t, client.deleteReservation(t.Context(), "e2e-test"), "deletion is idempotent")
}
