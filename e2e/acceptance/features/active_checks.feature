Feature: Active checks
  @gpu @soperator_version_>=4.2.0
  Scenario: GPU ActiveCheck succeeds on all GPU workers
    Given healthy GPU workers are available for active checks
    When the GPU ActiveCheck is triggered
    Then the GPU ActiveCheck Kubernetes Job succeeds
    And GPU ActiveCheck outputs report PASS on all GPU workers
    And GPU ActiveCheck Slurm jobs complete on all GPU workers
    And the GPU ActiveCheck status is Complete
