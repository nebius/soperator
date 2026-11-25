Feature: SSSD integration

  @soperator_version_>=5.0.0
  Scenario: Default SSSD configuration preserves local-user access
    Given SSSD is enabled without external configuration
    Then SSSD resolves an identity on login, controller, worker and enabled accounting
    And the local user resolves consistently on login, controller and worker
    And worker SSH without a job is allowed for the regular user
    When the local user connects from login to the worker over SSH
    Then the SSH connection reports the expected user and worker
    And a Slurm job submitted by that user succeeds
