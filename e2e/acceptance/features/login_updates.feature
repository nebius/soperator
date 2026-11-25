Feature: Delayed login updates
  # Sequential: Changes shared login update policy and replaces the login command endpoint.
  @soperator_version_>=5.1.0 @sequential
  Scenario: Login updates respect their minimum notice period
    Given the login workload is ready for a delayed update test
    When a login template update is scheduled with one minute of notice
    Then the outdated login pods remain until their scheduled restart
    And the updated login pods become ready

  # Sequential: Changes the shared login restart schedule and replaces the login command endpoint.
  @soperator_version_>=5.1.0 @sequential
  Scenario: Login updates wait for the configured cron window
    Given the login workload is ready for a delayed update test
    When a login template update is scheduled for the next cron window
    Then the outdated login pods remain until their scheduled restart
    And the updated login pods become ready

  # Sequential: Changes shared login update policy and restarts the login command endpoint during SSH checks.
  @soperator_version_>=5.1.0 @sequential
  Scenario: An administrator can restart an outdated login node after an interactive SSH warning
    Given the login workload is ready for a delayed update test
    When a login template update is scheduled with ten minutes of notice
    Then outdated login nodes warn only in interactive Bash SSH sessions
    When an administrator requests an early login restart
    Then the selected login pod is replaced before its scheduled restart
    And the selected login pod becomes ready at the target revision
