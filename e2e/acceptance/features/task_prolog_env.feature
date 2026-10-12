Feature: Task prolog environment

  @gpu @soperator_version_>=5.0.0
  Scenario: GPU jobs launched from login inherit NCCL network variables
    Given a healthy GPU worker is selected for task-prolog environment checks
    When a GPU Slurm job launched from the login node prints its network environment
    Then the job environment contains NCCL network variables
