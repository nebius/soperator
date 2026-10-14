Feature: Worker rolling update
  # Restarts the DaemonSets in soperator-system and leaves a changed worker
  # annotation on the selected NodeSet; each run sets a new value.
  @gpu @soperator_version_>=5.0.0
  Scenario: A worker template change rolls all workers after a Soperator system upgrade
    Given a ready GPU NodeSet with slurm-aware rolling update is selected
    And the Soperator system components have been upgraded since the workers started
    And a test job is running on one of its workers
    When the worker template of the NodeSet is changed
    Then the running job finishes without being killed
    And every worker pod of the NodeSet is replaced within the rollout budget
    And all workers of the NodeSet are idle with no leftover drain reason
    And the updated NodeSet accepts a targeted smoke job
