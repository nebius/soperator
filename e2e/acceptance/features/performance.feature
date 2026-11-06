Feature: Performance

  @heavy @soperator_version_>=5.0.0
  Scenario: A burst of batch jobs is accepted and drained within the budget
    Given the Slurm queue is idle
    And 128 free cores on 2 workers are reserved for the stress test user
    # Mixed CPU counts keep most jobs off the serial-job path that pack_serial_at_end optimizes.
    # Short sleeps let the reserved cores retire jobs almost as fast as the per-user RPC limit
    # admits them, so the controller handles the full submission and completion RPC stream.
    When the stress test user submits a burst of 5000 jobs with 1 to 3 CPUs sleeping up to 10 seconds
    # The budgets leave about 2x margin over runs of 2026-11-06 on two 80-core B200 workers with
    # Soperator 5.0.0 and Slurm 26.05.4: accepted in ~250 s (the per-user RPC rate limiter allows
    # about 20 sbatch per second), drained in 505-642 s with ~2500 jobs pending at the peak,
    # startup p99 2 s, steps p99 12 s, teardown p99 6 s, squeue mean 90-170 ms, scheduler cycle
    # mean 40 ms. Every 30 s the scheduler pass stalls whichever of squeue and sinfo it meets for
    # up to ~2 s, so their percentiles depend on probe phase: the mean bounds the typical squeue
    # latency, which grows with the queue depth, and the max bounds the stalls.
    Then every job in the burst is accepted within 8 minutes
    And every job in the burst completes successfully within 25 minutes
    And the node-side phases of the jobs stay within
      | startup p99  | 30s |
      | steps p99    | 30s |
      | teardown p99 | 15s |
    And the controller stays responsive throughout the load within
      | squeue mean | 1s |
      | squeue max  | 5s |
      | sinfo max   | 5s |
      | sdiag max   | 5s |
    And the scheduler statistics for the load stay within
      | main scheduler cycle mean | 1s |
      | server threads at the end | 10 |
