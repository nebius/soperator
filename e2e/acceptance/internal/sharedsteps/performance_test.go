package sharedsteps

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStressBurstScript(t *testing.T) {
	script := stressBurstScript("/work/burst", "e2e-stress-7", 1000, 3, 100)

	assert.Contains(t, script, "cd '/work/burst' || exit 1")
	assert.Contains(t, script, "seq 1 1000 | awk '{print ($1 % 3) + 1}' | xargs --max-procs=8 --replace={} 'sbatch' '--parsable' '--job-name=e2e-stress-7' '--reservation=e2e-stress-7' '--comment=skip_checks' "+
		"'--mem=256M' '--time=5' '--output=/dev/null' '--error=/dev/null' '--cpus-per-task={}' '--wrap=srun sleep \"$(shuf --input-range=1-100 --head-count=1)\"' >> ids.txt 2>> sbatch.err")
	assert.Contains(t, script, "echo accepted=$(cat ids.txt 2>/dev/null | wc --lines)")
}

func TestParseStateCounts(t *testing.T) {
	counts := parseStateCounts("squeue_ms=12\n    120 PENDING\n     40 RUNNING\n      3 PENDING\n")
	assert.Equal(t, map[string]int{"PENDING": 123, "RUNNING": 40}, counts)
	assert.Equal(t, 163, sumCounts(counts))
	assert.Equal(t, "PENDING=123 RUNNING=40", formatStateCounts(counts))
	assert.Empty(t, parseStateCounts(""))
	assert.Equal(t, "squeue --noheader --name='e2e-stress-burst-1' --format='%T' | sort | uniq --count", stressQueueStatesCommand("e2e-stress-burst-1"))
}

func TestChooseStressNodes(t *testing.T) {
	nodes := []partitionNode{
		{Name: "worker-0", Cores: 80, State: "idle"},
		{Name: "worker-1", Cores: 80, State: "idle"},
		{Name: "worker-2", Cores: 80, State: "drained"},
		{Name: "worker-cpu-0", Cores: 4, State: "idle"},
	}
	chosen, err := chooseStressNodes(nodes, 2, 64)
	require.NoError(t, err)
	assert.Equal(t, []string{"worker-0", "worker-1"}, chosen)

	chosen, err = chooseStressNodes(nodes, 4, 64)
	require.NoError(t, err)
	assert.Nil(t, chosen, "too few large nodes means skip")

	nodes[1].State = "mixed"
	_, err = chooseStressNodes(nodes, 2, 64)
	require.ErrorContains(t, err, "only 1 of 3 nodes with at least 64 cores are idle, need 2: worker-1=mixed, worker-2=drained")
}

func TestCancelStressUserJobsCommand(t *testing.T) {
	assert.Equal(t, "! id 'stress' >/dev/null 2>&1 || scancel --user='stress'", cancelStressUserJobsCommand(),
		"a fresh cluster has no stress user yet, and scancel rejects an unknown user name")
}

func TestStressReservationCommand(t *testing.T) {
	assert.Equal(t,
		"'scontrol' 'create' 'reservation' 'ReservationName=e2e-stress-7' 'Users=stress' 'PartitionName=main' "+
			"'StartTime=now' 'Duration=40' 'Nodes=worker-0,worker-1' 'CoreCnt=128'",
		stressReservationCommand("e2e-stress-7", []string{"worker-0", "worker-1"}, 128, 40*time.Minute))
}

func TestStaleStressReservations(t *testing.T) {
	output := "ReservationName=e2e-stress-1793814313593480000 StartTime=2026-11-04T17:45:13 Nodes=worker-[0-1] Users=stress\n" +
		"ReservationName=maintenance-weekly StartTime=2026-11-05T00:00:00 Nodes=worker-0 Users=root\n" +
		"ReservationName=e2e-stress-1793814320000000000 StartTime=2026-11-04T17:50:00 Nodes=worker-[0-1] Users=stress\n"
	assert.Equal(t, []string{"e2e-stress-1793814313593480000", "e2e-stress-1793814320000000000"}, staleStressReservations(output))
	assert.Empty(t, staleStressReservations("No reservations in the system\n"))
}

func TestReservationCoreSplit(t *testing.T) {
	shown := "Reservation created: e2e-stress-7\nReservationName=e2e-stress-7 Nodes=worker-[0-1] NodeCnt=2 CoreCnt=128 Features=(null) PartitionName=main Flags=   " +
		"NodeName=worker-0 CoreIDs=0-79   NodeName=worker-1 CoreIDs=0-47 TRES=cpu=256 Users=stress"
	assert.Equal(t, []string{"worker-0=0-79", "worker-1=0-47"}, reservationCoreSplit(shown))
	assert.Equal(t, "128", extractField(shown, "CoreCnt"))
}

func TestParseStressSubmitResult(t *testing.T) {
	result, err := parseStressSubmitResult("start=1000.25\nend=1052.75\nstatus=0\naccepted=1000\n")
	require.NoError(t, err)
	assert.Equal(t, stressSubmitResult{
		StartEpoch: 1000.25,
		EndEpoch:   1052.75,
		Accepted:   1000,
		ExitStatus: 0,
	}, result)
	assert.Equal(t, 52500*time.Millisecond, result.Duration())

	_, err = parseStressSubmitResult("start=1000\nend=1052\n")
	require.ErrorContains(t, err, "lacks status")

	_, err = parseStressSubmitResult("start=abc\nend=1\nstatus=0\naccepted=1")
	require.ErrorContains(t, err, `parse submission start "abc"`)
}

func TestEvaluateStressSubmit(t *testing.T) {
	good := stressSubmitResult{StartEpoch: 0, EndEpoch: 50, Accepted: 1000}
	require.NoError(t, evaluateStressSubmit(good, 1000, time.Minute))

	bad := stressSubmitResult{StartEpoch: 0, EndEpoch: 90, Accepted: 998, ExitStatus: 123}
	err := evaluateStressSubmit(bad, 1000, time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepted 998 of 1000 submissions")
	assert.Contains(t, err.Error(), "exited with status 123")
	assert.Contains(t, err.Error(), "submission took 1m30s, budget 1m0s")
}
