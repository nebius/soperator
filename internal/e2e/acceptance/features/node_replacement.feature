Feature: Node replacement
  @gpu
  @unstable
  Scenario: A maintenance event replaces the selected worker node
    Given a test job is submitted and running on a worker node
    When a maintenance event is triggered for that node
    Then the node is drained with a maintenance reason
    When the test job is cancelled
    Then the old instance is removed
    And a replacement node joins the cluster
    And the replacement node passes GPU validation

  @heterogeneous
  @unstable
  Scenario: A maintenance event replaces a CPU worker node in a heterogeneous cluster
    Given a test job is submitted and running on a CPU worker node
    When a maintenance event is triggered for that node
    Then the node is drained with a maintenance reason
    When the test job is cancelled
    Then the old instance is removed
    And a replacement node joins the cluster
    And all pre-existing worker nodes are operational
