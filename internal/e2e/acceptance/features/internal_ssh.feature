Feature: Internal SSH
  Scenario: A regular user can SSH to a worker without extra options
    Given a regular user account exists on the login node
    And the selected worker host key is not present for that user
    When the user SSHs from the login node to a worker
    Then the connection succeeds without extra SSH options
