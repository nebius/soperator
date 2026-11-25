Feature: Recovery after a stuck job

  @heavy @soperator_version_>=5.0.0
  Scenario: A worker recovers after Slurm cannot kill a job
    Given an idle GPU worker is reserved for stuck-job recovery
    When a stuck-job drain is simulated on that worker
    Then the worker goes down and reboots
    And the worker returns to service automatically
    And a job can use all GPUs on that worker
