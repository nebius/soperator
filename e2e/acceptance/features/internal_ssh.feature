Feature: Internal SSH
  @essential @soperator_version_>=4.0.0
  Scenario: A regular user with a job can SSH to a worker without extra options
    Given a regular user account exists on the login node
    And the user has a running job on the worker
    When the user SSHs from the login node to a worker
    Then the connection succeeds without extra SSH options
