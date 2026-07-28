Feature: Active checks
  Scenario: Logs cleaner ActiveCheck removes old output files
    Given an old soperator output file is created for acceptance
    When the logs cleaner ActiveCheck is triggered
    Then the logs cleaner Kubernetes Job succeeds
    And the logs cleaner ActiveCheck status is Complete
    And the old soperator output file is removed

  @gpu
  Scenario: GPU ActiveCheck succeeds on all GPU workers
    Given healthy GPU workers are available for active checks
    When the GPU ActiveCheck is triggered
    Then the GPU ActiveCheck Kubernetes Job succeeds
    And GPU ActiveCheck Slurm jobs complete on all GPU workers
    And the GPU ActiveCheck status is Complete
    And GPU ActiveCheck outputs report PASS on all GPU workers
