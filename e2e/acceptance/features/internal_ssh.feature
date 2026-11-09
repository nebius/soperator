Feature: Internal SSH
  @soperator_version_>=4.0.0
  Scenario: A regular user can SSH to a worker without extra options
    Given a regular user account exists on the login node
    And worker SSH without a job is allowed for the regular user
    When the user SSHs from the login node to a worker
    Then the connection succeeds without extra SSH options

  @gpu @soperator_version_>=5.0.0
  Scenario: Worker SSH is adopted into a running job
    Given PAM Slurm adopt is enabled
    And the effective Slurm configuration is read
    And it contains the following settings:
      | setting          | value                                    |
      | PrologFlags      | Alloc,Contain                            |
      | LaunchParameters | use_interactive_step,ulimit_pam_adopt   |
    And a PAM Slurm adopt test user and GPU worker are ready
    Then SSH to the worker without a job is denied
    When the user starts a GPU job and opens SSH to its worker
    Then the SSH session is adopted and ends with the job
