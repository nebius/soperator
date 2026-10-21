Feature: System settings

  @soperator_version_>=4.0.0
  Scenario: Login SSH sessions expose the default resource-limit profile
    When root collects direct and nested Bash resource limits over SSH to the login node
    Then direct and nested login SSH resource limits match the expected default profile

  @soperator_version_>=5.0.0
  Scenario: Worker SSH sessions expose the compute resource-limit profile
    Given a healthy worker is selected for system-settings checks
    When root collects direct and nested Bash resource limits over SSH to the worker
    Then direct and nested worker SSH resource limits match the compute profile

  @essential @soperator_version_>=5.0.0
  Scenario: Native Slurm jobs expose the compute resource-limit profile
    Given a healthy worker is selected for system-settings checks
    When a native Slurm job collects direct and nested Bash resource limits
    Then the native system-settings job succeeds
    And its direct and nested resource limits match the Slurm compute profile

  @soperator_version_>=5.0.0
  Scenario: Enroot Slurm jobs expose the compute resource-limit profile
    Given a healthy worker is selected for system-settings checks
    When an Enroot Slurm job collects direct and nested Bash resource limits
    Then the Enroot system-settings job succeeds
    And its direct and nested resource limits match the Slurm compute profile

  @essential @soperator_version_>=4.0.0
  Scenario: Required kernel settings are visible in SSH sessions and Slurm jobs
    Given a healthy worker is selected for system-settings checks
    When kernel settings are collected from login and worker SSH sessions
    Then both SSH sessions expose the required kernel settings
    When native and Enroot Slurm jobs collect kernel settings
    Then both kernel-settings jobs succeed
    And both jobs expose the required kernel settings

  @essential @gpu @soperator_version_>=4.0.0
  Scenario: GPU workers expose the required NVIDIA driver capabilities
    Given a healthy GPU worker is selected for system-settings checks
    Then its Slurm container exposes the required NVIDIA driver capabilities

  @soperator_version_>=4.0.0
  Scenario: Regular-user worker SSH sessions expose the compute resource-limit profile
    Given a healthy worker is selected for system-settings checks
    And a regular user account is available on the worker
    And worker SSH without a job is allowed for the regular user
    When the regular user collects direct and nested Bash resource limits over SSH to the worker
    Then direct and nested worker SSH resource limits match the regular-user compute profile

  @soperator_version_>=5.0.0
  Scenario: Regular-user Slurm jobs expose the compute resource-limit profile
    Given a healthy worker is selected for system-settings checks
    And a regular user account is available on the worker
    When the regular user submits a native Slurm job that collects direct and nested Bash resource limits
    Then the native system-settings job succeeds as the regular user
    And its direct and nested resource limits match the Slurm compute profile

  @soperator_version_>=5.0.0
  Scenario: Docker Slurm jobs expose the compute resource-limit profile
    Given a healthy worker is selected for system-settings checks
    When a Docker Slurm job collects direct and nested Bash resource limits
    Then the Docker system-settings job succeeds
    And its direct and nested resource limits match the compute profile

  @soperator_version_>=5.0.0
  Scenario: Required kernel settings are visible in Docker Slurm jobs
    Given a healthy worker is selected for system-settings checks
    When a Docker Slurm job collects kernel settings
    Then the Docker kernel-settings job succeeds
    And it exposes the required kernel settings

  @soperator_version_>=5.0.0
  Scenario: Regular-user worker SSH adopted into a job exposes the Slurm compute profile
    Given a healthy worker is selected for system-settings checks
    And a regular user account is available on the worker
    And PAM Slurm adopt requires a job for the regular user
    When the regular user starts a job on the worker and collects direct and nested Bash resource limits over SSH
    Then direct and nested adopted SSH resource limits match the regular-user Slurm compute profile

  @soperator_version_>=4.0.0
  Scenario: Regular-user login SSH over the public endpoint exposes login limits and kernel settings
    Given the login node has a public SSH endpoint
    And a regular user can log in over the public SSH endpoint
    When the regular user collects resource limits and kernel settings over public login SSH
    Then direct and nested login SSH resource limits match the expected default profile
    And the public login SSH session exposes the required kernel settings

  @soperator_version_>=5.0.0
  Scenario: Regular-user interactive jobs expose compute limits and kernel settings
    Given a healthy worker is selected for system-settings checks
    And a regular user account is available on the worker
    When the regular user runs native and Enroot interactive jobs that collect resource limits and kernel settings
    Then both interactive jobs report their resource limits and kernel settings
    And both interactive jobs' resource limits match the Slurm compute profile
    And both interactive jobs expose the required kernel settings
