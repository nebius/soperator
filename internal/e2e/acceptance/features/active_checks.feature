Feature: Active checks
  Scenario: Logs cleaner ActiveCheck removes old output files
    Given an old soperator output file is created for acceptance
    When the logs cleaner ActiveCheck is triggered
    Then the old soperator output file is removed

  @gpu
  Scenario: GPU ActiveCheck succeeds on all GPU workers
    Given GPU workers are available for active checks
    When the GPU ActiveCheck is triggered
    Then the GPU ActiveCheck finishes successfully on all GPU workers
