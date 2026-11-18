Feature: Recovery after a stuck job

  @heavy @soperator_version_>=5.0.0
  Scenario: A worker recovers after Slurm cannot kill a job
    Given an idle GPU worker is reserved for stuck-job recovery
    When a stuck-job drain is simulated on that worker
    Then the worker goes down and reboots
    And the worker returns to service automatically
    And a job can use all GPUs on that worker

  @heavy @soperator_version_>=5.0.0
  Scenario: Monitoring records a worker recovering after Slurm cannot kill a job
    Given an idle GPU worker is reserved for stuck-job recovery
    And monitoring is recording that worker
    When a stuck-job drain is simulated on that worker
    Then the worker goes down and reboots
    And the worker returns to service automatically
    And a job can use all GPUs on that worker
    And monitoring records the stuck-job drain, down state and recovery
