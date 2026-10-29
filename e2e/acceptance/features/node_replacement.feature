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
    And all pre-existing worker nodes are operational

  # @manual: the platform replaces the node only after Compute SetUnhealthy succeeds, which taints
  # a real bare-metal host and takes the tenant's unhealthy-node slot until the host is returned.
  # Run it by name with --run-heavy. Drop the tag once Mk8s honours nebius.com/skip-set-unhealthy for
  # HardwareIssuesSuspected and the e2e worker node groups carry that label.
  @manual @heavy @gpu @soperator_version_>=5.0.0
  Scenario: A failed GPU health check replaces the worker node
    Given a test job is submitted and running on a GPU worker node
    When a GPU health check failure is simulated on that node
    Then the node is drained with a node problem reason
    When the test job is cancelled
    Then the old instance is removed
    And a replacement node joins the cluster
    And the replacement node passes GPU validation

  # @manual: the health-check failure can report a real bare-metal host through Compute SetUnhealthy
  # and occupy the tenant's unhealthy-node slot. Run explicitly with --run-heavy.
  # Drop the tag once Mk8s honours nebius.com/skip-set-unhealthy for HardwareIssuesSuspected
  # and the e2e worker node groups carry that label.
  @manual @heavy @gpu @soperator_version_>=5.0.0
  Scenario: A maintenance event replaces a GPU worker already draining after a health check failure
    Given a test job is submitted and running on an idle GPU worker node
    When a GPU health check failure is simulated on that node
    Then the node is draining with a node problem reason while the test job is running
    When a maintenance event is triggered while that node is draining
    Then the node is draining with a maintenance reason while the test job is running
    When the test job is cancelled
    Then the old instance is removed
    And a replacement node joins the cluster
    And the replacement node passes GPU validation

  # @manual: the health-check failure can report a real bare-metal host through Compute SetUnhealthy
  # and occupy the tenant's unhealthy-node slot. Run explicitly with --run-heavy.
  # Drop the tag once Mk8s honours nebius.com/skip-set-unhealthy for HardwareIssuesSuspected
  # and the e2e worker node groups carry that label.
  @manual @heavy @gpu @soperator_version_>=5.0.0
  Scenario: A maintenance event replaces a GPU worker fully drained after a health check failure
    Given a test job is submitted and running on an idle GPU worker node
    When a GPU health check failure is simulated on that node
    Then the node is draining with a node problem reason while the test job is running
    When the test job is cancelled
    And a maintenance event is triggered after that node has fully drained
    Then the node is drained with a maintenance reason
    And the old instance is removed
    And a replacement node joins the cluster
    And the replacement node passes GPU validation
