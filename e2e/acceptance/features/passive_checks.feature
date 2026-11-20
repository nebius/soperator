Feature: Passive checks
  @soperator_version_>=4.0.0
  Scenario: CPU jobs run expected Prolog and Epilog passive checks
    Given a worker is selected
    When a CPU-only Slurm job runs on the selected worker
    Then the CPU job Prolog check runner output is fresh and healthy
    And the CPU job Epilog check runner output is fresh and healthy
    And GPU-only passive checks are not executed for the CPU job

  @soperator_version_>=4.0.0
  Scenario: drop_page_cache runs after CPU jobs
    Given a worker is selected
    When a CPU-only Slurm job runs on the selected worker
    Then the drop_page_cache passive check completed in Epilog

  @soperator_version_>=4.0.0,<5.0.0
  Scenario: Passive Prolog drains a worker when allocated memory exceeds available memory
    Given a worker is selected
    When memory pressure is created on the selected worker
    And an all-memory Slurm job is submitted to the selected worker
    Then the selected worker is drained by alloc_mem_used
    When the memory pressure is removed
    And HealthCheckProgram runs on the selected worker
    Then the selected worker no longer has alloc_mem_used reason
    And the selected worker is usable after alloc_mem_used

  @soperator_version_>=5.0.0
  Scenario: HealthCheckProgram drains an idle worker when available memory is below RealMemory
    Given a worker is selected
    When memory pressure is created on the selected worker
    And HealthCheckProgram runs on the selected worker
    Then the selected worker is drained by alloc_mem_used
    When the memory pressure is removed
    And HealthCheckProgram runs on the selected worker
    Then the selected worker no longer has alloc_mem_used reason
    And the selected worker is usable after alloc_mem_used

  @gpu @soperator_version_>=5.0.0
  Scenario: GPU jobs run passive GPU health checks
    Given a GPU worker is selected
    When a small GPU Slurm job runs on the selected GPU worker
    Then the GPU job health-check Prolog report is fresh and passing
    And the GPU job health-check Epilog report is fresh and passing
    When HealthCheckProgram runs on the selected GPU worker
    Then the GPU HealthCheckProgram report is fresh and passing
    And raw GPU health-check command outputs are present

  @gpu @soperator_version_>=5.0.0
  Scenario: A full-node multi-node GPU job runs passive GPU health checks on every node
    Given two idle GPU workers are selected
    When a Slurm job using every GPU on both workers runs
    Then each worker's Prolog GPU health-check report for the job is fresh and passing
    And each worker's Epilog GPU health-check report for the job is fresh and passing

  @gpu @soperator_version_>=5.0.0
  Scenario: GPU workers pass the nvidia_libraries check in HealthCheckProgram and Prolog
    Given a GPU worker is selected
    When HealthCheckProgram runs on the selected worker
    Then the nvidia_libraries check passes in HealthCheckProgram on the selected GPU worker
    When a CPU-only Slurm job runs on the selected worker
    Then the CPU job Prolog check runner output is fresh and healthy
    And the nvidia_libraries check passed in the CPU job Prolog

  # The fault is a fake file bind-mounted over the driver library in one worker's slurmd mount namespace;
  # the shared jail and the other workers keep the real library. "missing" needs a dangling soname
  # symlink on the shared jail, which would drain every GPU worker, so it is covered by
  # nvidia_libraries_test.py only.
  @heavy @gpu @soperator_version_>=5.0.0
  Scenario Outline: nvidia_libraries drains a GPU worker whose <library> is <failure> and recovers after restoration
    Given a GPU worker is selected
    And the periodic HealthCheckProgram has just completed on the selected GPU worker
    When <library> is replaced with <fault> on the selected GPU worker only
    And a CPU-only Slurm job is submitted to the selected GPU worker
    Then the CPU-only job is requeued without running
    And the selected GPU worker is drained by nvidia_libraries because <library> is <failure> in Prolog
    When the NVIDIA library fault is removed from the selected GPU worker
    And HealthCheckProgram runs on the selected worker
    Then the selected GPU worker no longer has the nvidia_libraries reason
    And the selected GPU worker is usable after nvidia_libraries

    Examples:
      | library           | fault            | failure        |
      | libcuda.so.1      | a truncated file | file too short |
      | libnvidia-ml.so.1 | a corrupt file   | not loadable   |

  # Sequential: Asserts periodic GPU health on every GPU worker selected outside shared ownership.
  @gpu @soperator_version_>=5.0.0 @sequential
  Scenario: Periodic HealthCheckProgram passes GPU health checks on all GPU workers
    Given the powered-up GPU workers are selected for periodic health checks
    When a new periodic GPU health check completes on every selected GPU worker
    Then every new periodic GPU health-check report is passing
    And every new periodic GPU health-check report has metadata matching its worker
    And every new periodic GPU health-check report has its raw command outputs

  @soperator_version_>=4.0.0,<5.0.0
  Scenario: Job tmpfs directory is scoped to the Slurm job lifetime
    Given a worker is selected
    When a Slurm job checks its job tmpfs directory on the selected worker
    Then the job tmpfs directory existed during the job
    And the job tmpfs directory is removed after the job exits

  @gpu @soperator_version_>=5.0.0
  Scenario: Passive Prolog drains a worker with unmanaged GPU processes
    Given a GPU worker is selected
    When an unmanaged GPU workload is started on the selected GPU worker
    And a full-node GPU Slurm job is submitted to the selected GPU worker
    Then the selected GPU worker is drained by alloc_gpus_busy
    When the unmanaged GPU workload is stopped
    And HealthCheckProgram runs on the selected worker
    Then the selected GPU worker no longer has alloc_gpus_busy reason
    And the selected GPU worker is usable after alloc_gpus_busy
