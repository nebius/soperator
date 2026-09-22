Feature: PAM Slurm adopt

  @gpu @unstable @soperator_version_>=5.0.0
  Scenario: Worker SSH is adopted into a running job
    Given the effective Slurm configuration is read
    And it contains the following settings:
      | setting          | value                                    |
      | PrologFlags      | Alloc,Contain                            |
      | LaunchParameters | use_interactive_step,ulimit_pam_adopt   |
    And a PAM Slurm adopt test user and GPU worker are ready
    Then SSH to the worker without a job is denied
    When the user starts a GPU job and opens SSH to its worker
    Then the SSH session is adopted and ends with the job
