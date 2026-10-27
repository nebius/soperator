Feature: Active checks
  @heavy @gpu @soperator_version_>=5.0.0
  Scenario: GPU ActiveCheck succeeds on all GPU workers
    Given healthy GPU workers are available for active checks
    When the GPU ActiveCheck is triggered
    Then the GPU ActiveCheck Kubernetes Job succeeds
    And GPU ActiveCheck outputs report PASS on all GPU workers
    And GPU ActiveCheck Slurm jobs complete on all GPU workers
    And the GPU ActiveCheck status is Complete

  @heavy @soperator_version_>=5.0.0
  Scenario: Manage jail dry-run detects drift without modifying the jail
    Given the manage-jail-state dry-run ActiveCheck is configured
    And harmless drift is introduced into a managed jail file
    When the manage-jail-state dry-run ActiveCheck is triggered
    Then the manage-jail-state dry-run Kubernetes Job succeeds
    And Ansible reports the managed jail file as changed
    And Ansible reports no failed or unreachable hosts
    And the managed jail file remains unchanged by the dry-run
    And the manage-jail-state dry-run ActiveCheck status is Complete
