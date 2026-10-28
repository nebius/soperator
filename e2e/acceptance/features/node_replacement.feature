Feature: Node replacement
  @heavy @gpu @unstable @soperator_version_>=5.0.0
  Scenario: A maintenance event replaces the selected worker node
    Given a test job is submitted and running on a GPU worker node
    When a maintenance event is triggered for that node
    Then the node is drained with a maintenance reason
    When the test job is cancelled
    Then the old instance is removed
    And a replacement node joins the cluster
    And the replacement node passes GPU validation

  @heavy @cpu @gpu @unstable @soperator_version_>=5.0.0
  Scenario: A maintenance event replaces a CPU worker node when CPU and GPU workers exist
    Given a test job is submitted and running on a CPU worker node
    When a maintenance event is triggered for that node
    Then the node is drained with a maintenance reason
    When the test job is cancelled
    Then the old instance is removed
    And a replacement node joins the cluster
    And the replacement CPU worker is operational

  @heavy @gpu @soperator_version_>=5.0.0
  Scenario: A failed GPU health check replaces the worker node
    Given a test job is submitted and running on a GPU worker node
    When a GPU health check failure is simulated on that node
    Then the node is drained with a node problem reason
    When the test job is cancelled
    Then the old instance is removed
    And a replacement node joins the cluster
    And the replacement node passes GPU validation
