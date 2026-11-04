Feature: Local NVMe scratch
  # Sequential: Selects NVMe workers from snapshots and runs direct srun jobs outside worker reservations and job cleanup tracking.
  @soperator_version_>=5.0.0 @sequential
  Scenario: Workers expose local NVMe scratch to jobs
    Given the cluster has NodeSets with local NVMe scratch
    And at most 2 local NVMe NodeSets with at most 2 workers each are selected
    And a regular user account is available on the selected local NVMe workers
    Then the scratch size limit matches the slurmd ephemeral storage request on every selected local NVMe NodeSet
    And every selected local NVMe worker mounts scratch on a dedicated filesystem
    And a regular user's job writes and reads back a file in scratch on every selected local NVMe worker
    And a regular user's Pyxis job reads the scratch file on every selected local NVMe worker
