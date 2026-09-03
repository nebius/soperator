Feature: Slurm configuration updates

  @soperator_version_>=4.0.0
  Scenario: A custom Slurm setting is applied and restored
    Given at least one active worker is available for Slurm reconfiguration
    And the current custom Slurm configuration is recorded
    Then worker start times remain unchanged while the Slurm configuration is unchanged
    When JobRequeue is overridden to 0 in the SlurmCluster
    Then the effective JobRequeue value becomes 0
    And every active worker has been reconfigured
    When the original custom Slurm configuration is restored
    Then the effective JobRequeue value is restored
    And every active worker has been reconfigured again
