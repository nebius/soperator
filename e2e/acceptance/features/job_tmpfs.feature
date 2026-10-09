Feature: Job mount namespaces
  @soperator_version_>=5.0.0
  Scenario: Concurrent jobs have isolated and accounted temporary memory
    Given two tmpfs probe jobs run on the same worker
    Then each job shares its temporary files across steps but not with the other job
    And sbcast and srun broadcast files into each job's temporary filesystems
    And Pyxis can access the job temporary files
    When the first tmpfs job exits normally and the second is cancelled
    Then both job mount namespaces and memory cgroups are removed
