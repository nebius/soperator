Feature: Performance

  # The load is meant to stress the controller: thousands of tiny jobs make it handle the submission,
  # scheduling and completion RPC stream at the rate the per-user limiter allows. The workers can be the
  # bottleneck instead: with up to a hundred jobs starting at once on a reserved worker, slurmd serializes
  # the job cgroup setup, and that gets slower as finished jobs leave dying cgroups behind (the stress jobs
  # skip the epilog cache drop). The node-side phase budgets tell the two apart from the controller budgets.
  @heavy @soperator_version_>=5.0.0
  Scenario: A burst of batch jobs is accepted and drained within the budget
    Given Slurm accounting is reachable and the cluster is registered
    And the Slurm queue is idle
    # The cores are reserved inside a whole-node reservation of the same workers, so the load
    # runs on a fixed number of cores while nothing else, GPU jobs included, shares the workers.
    And 128 free cores on 2 workers are reserved for the stress test user
    # Mixed core counts keep most jobs off the serial-job path that pack_serial_at_end optimizes.
    # Short sleeps let the reserved cores retire jobs almost as fast as the per-user RPC limit
    # admits them, so the controller handles the full submission and completion RPC stream.
    When the stress test user submits a burst of 5000 jobs with 1 to 3 cores sleeping up to 10 seconds
    # The budgets leave about 2x margin over the values measured on two-worker B200, H100/H200 and B300
    # clusters with Soperator 5.0.0 and Slurm 26.05.4. Acceptance is bounded by the per-user RPC rate limiter
    # (about 20 sbatch per second). Every 30 s the scheduler pass holds the job and node locks for a few
    # seconds and stalls the probes it meets, so the means bound the typical latency and the maxima the stalls.
    Then every job in the burst is accepted within 6 minutes
    And every job in the burst completes successfully within 20 minutes
    And the node-side phases of the jobs stay within
      # Job start to first srun step start: prolog, batch script start and step launch.
      | startup p99  | 10s |
      # First step start to last step end: the srun sleep itself.
      | steps p99    | 20s |
      # Last step end to extern step end: slurmd killing and reaping the job's steps, without epilog.
      | teardown p99 | 10s |
    And the controller stays responsive throughout the load within
      | squeue mean | 1s  |
      | sinfo mean  | 1s  |
      | squeue max  | 10s |
      | sinfo max   | 10s |
      | sdiag max   | 10s |
    And the scheduler statistics for the load stay within
      | main scheduler cycle mean     | 200ms |
      | backfill scheduler cycle mean | 200ms |
      | server threads at the end     | 5     |
