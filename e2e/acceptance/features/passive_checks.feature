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

  @soperator_version_>=4.0.0
  Scenario: Passive Prolog drains a worker when allocated memory exceeds available memory
    Given a worker is selected
    When memory pressure is created on the selected worker
    And an all-memory Slurm job is submitted to the selected worker
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
    And raw GPU health-check command outputs are present

  @soperator_version_>=4.0.0
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
