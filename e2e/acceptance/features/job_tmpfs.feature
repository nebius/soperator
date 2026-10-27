Feature: Job tmpfs volumes
  @soperator_version_>=5.0.0 @parallel
  Scenario: Concurrent jobs have isolated and accounted temporary memory
    Given generated job tmpfs is enabled
    And two tmpfs probe jobs run on the same worker
    Then each job shares its temporary files across steps but not with the other job
    And sbcast broadcasts files into each job's temporary filesystems
    And Pyxis can access the job temporary files
    When the first tmpfs job exits normally and the second is cancelled
    Then both job mount namespaces and memory cgroups are removed
