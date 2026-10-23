Feature: Login user isolation
  @soperator_version_>=5.0.0
  Scenario: Additional processes do not increase a login user's CPU share
    Given two regular users can SSH to the login node for CPU isolation testing
    When both users run workloads at twice the login CPU capacity
    And the first user runs at login CPU capacity while the second runs at four times capacity
    Then both users complete similar amounts of work in both cases
    And the first user's amount of completed work remains stable
