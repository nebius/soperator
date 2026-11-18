Feature: Static worker power down
  # Deprecated: covers the static worker feature, which will be removed. Once a release drops it, limit
  # this scenario with the version tag to the releases that still have it, and delete the scenario
  # when none of them is tested.
  # TODO: The first step skips builds whose NodeSetPowerState CRD has no spec.recreateNodes, because main and
  # soperator-release-5.0 are both 5.0.0. Replace that check with a version constraint once the first
  # release that recreates powered down static workers is known.
  @heavy @soperator_version_>=5.0.0
  Scenario: A manually powered down static worker returns to service
    Given an idle static worker is selected for a power down
    When the selected static worker is powered down through Slurm
    Then Slurm powers the selected static worker down
    And the selected static worker returns to service in a recreated pod
    And the selected static worker runs a job
