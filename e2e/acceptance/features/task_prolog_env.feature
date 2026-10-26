Feature: Task prolog environment

  @gpu @soperator_version_>=5.0.0
  Scenario: GPU jobs and worker SSH sessions inherit NCCL network variables
    Given task-prolog environment injection is enabled
    And a healthy GPU worker is selected for NCCL environment checks
    When a GPU Slurm job launched from the login node prints its network environment
    And a regular user SSH login shell on the worker prints its network environment
    Then the job environment contains NCCL network variables
    And the worker SSH session contains NCCL network variables
