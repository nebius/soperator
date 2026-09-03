Feature: Cluster creation
  Scenario: The provisioned cluster is ready for acceptance tests
    Then all non-job pods in soperator are Running and Ready
    # The soperator-activechecks HelmRelease becomes Ready only after its post-install
    # hook accepts the initial runAfterCreation ActiveChecks.
    And all HelmReleases are Ready
    And all SlurmCluster CRs are available
    And all NodeSet CRs are ready
    And configured nodesets match the live cluster
    And main and hidden partitions are present and sane
    And all Slurm nodes are healthy
    And login welcome output shows cluster information
    And main partition smoke job succeeds
    And hidden partition smoke job succeeds
    And each configured nodeset accepts a targeted smoke job
