Feature: Default Slurm configuration

  @soperator_version_>=4.0.0
  Scenario: Stable Soperator defaults are applied
    When the effective Slurm configuration is read
    Then it contains the following settings:
      | setting          | value                     |
      | MpiDefault       | pmix                      |
      | ProctrackType    | proctrack/cgroup          |
      | TaskPlugin       | task/cgroup,task/affinity |
      | SelectType       | select/cons_tres          |
      | ReturnToService  | 2                         |
      | SchedulerType    | sched/backfill            |
      | JobRequeue       | 1                         |
      | SlurmdTimeout    | 180 sec                   |
    And its cluster name matches the target cluster
    And main partition smoke job succeeds
