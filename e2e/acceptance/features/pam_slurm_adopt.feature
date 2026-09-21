Feature: PAM Slurm adopt

  @gpu @unstable @soperator_version_>=5.0.0
  Scenario: Worker SSH is adopted into a running job
    Given PAM Slurm adopt is enabled for worker SSH
    And a PAM Slurm adopt test user and GPU worker are ready
    Then SSH to the worker without a job is denied
    When a long-running GPU job owned by the user starts on the worker
    And the user opens a long-running SSH session to the worker
    Then the SSH session is in the job extern cgroup
    And the SSH session sees only the job's allocated GPU
    When the PAM Slurm adopt job is cancelled
    Then the adopted SSH session terminates
