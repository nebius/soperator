Feature: System settings

  @soperator_version_>=4.0.0
  Scenario: Login SSH sessions expose the default resource-limit profile
    When root collects direct and nested Bash resource limits over SSH to the login node
    Then direct and nested login SSH resource limits match the expected default profile

  # SCHED-2442: remove @unstable after root worker SSH and Slurm jobs receive the compute limit profile.
  @unstable @soperator_version_>=4.0.0
  Scenario: Worker SSH sessions expose the compute resource-limit profile
    Given a healthy worker is selected for system-settings checks
    When root collects direct and nested Bash resource limits over SSH to the worker
    Then direct and nested worker SSH resource limits match the compute profile

  @unstable @soperator_version_>=4.0.0
  Scenario: Native Slurm jobs expose the compute resource-limit profile
    Given a healthy worker is selected for system-settings checks
    When a native Slurm job collects direct and nested Bash resource limits
    Then the native system-settings job succeeds
    And its direct and nested resource limits match the compute profile

  @unstable @soperator_version_>=4.0.0
  Scenario: Enroot Slurm jobs expose the compute resource-limit profile
    Given a healthy worker is selected for system-settings checks
    When an Enroot Slurm job collects direct and nested Bash resource limits
    Then the Enroot system-settings job succeeds
    And its direct and nested resource limits match the compute profile

  @soperator_version_>=4.0.0
  Scenario: Required kernel settings are visible in SSH sessions and Slurm jobs
    Given a healthy worker is selected for system-settings checks
    When kernel settings are collected from login and worker SSH sessions
    Then both SSH sessions expose the required kernel settings
    When native and Enroot Slurm jobs collect kernel settings
    Then both kernel-settings jobs succeed
    And both jobs expose the required kernel settings

  @gpu @soperator_version_>=4.0.0
  Scenario: GPU workers expose the required NVIDIA driver capabilities
    Given a healthy GPU worker is selected for system-settings checks
    Then its Slurm container exposes the required NVIDIA driver capabilities
