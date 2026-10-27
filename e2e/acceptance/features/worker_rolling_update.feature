Feature: Slurm-aware worker rolling updates
  @heavy @soperator_version_>=5.0.0
  Scenario: A worker template update preserves running jobs
    Given test jobs are running on two workers of a NodeSet with slurmAwareRollingUpdate strategy and maxUnavailable at least 2
    When the Soperator system DaemonSets are restarted
    And the worker template of the NodeSet is changed
    Then the first worker is drained with a rolling update reason
    And the second worker is drained with a rolling update reason
    When all test jobs finish without interruption
    Then all active workers of the NodeSet are idle
    And the updated NodeSet accepts smoke jobs

  @heavy @soperator_version_>=5.0.0 @parallel
  Scenario: An MK8s node group update preserves running jobs
    Given test jobs are running on two workers of a NodeSet with slurmAwareRollingUpdate strategy and maxUnavailable at least 2
    When the MK8s node group specification is changed to replace its nodes
    Then the first worker is drained with a rolling update reason
    And the second worker is drained with a rolling update reason
    When all test jobs finish without interruption
    Then all active workers of the NodeSet are idle
    And the updated NodeSet accepts smoke jobs
    When the original MK8s node group specification is restored
    Then the updated NodeSet accepts smoke jobs

  @heavy @soperator_version_>=5.0.0 @parallel
  Scenario: A worker update preserves maintenance drains before and during handoff
    Given test jobs are running on two workers of a NodeSet with slurmAwareRollingUpdate strategy and maxUnavailable at least 2
    # TODO: Configure --requeue-after-slurm-nodes in the Helm chart for E2E clusters
    # to reduce the wait for maintenance reconciliation.
    When a maintenance event is triggered for the first worker
    Then the first worker is drained with a maintenance reason
    When the worker template of the NodeSet is changed
    Then the second worker is drained with a rolling update reason
    And the first worker is drained with a maintenance reason
    When a maintenance event is triggered for the second worker
    Then the first worker is drained with a maintenance reason
    And the second worker is drained with a maintenance reason
    When all test jobs finish without interruption
    Then all active workers of the NodeSet are idle
    And the updated NodeSet accepts smoke jobs
