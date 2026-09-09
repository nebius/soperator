Feature: Slurm block-topology scheduling
  # Deliberately left out of FeaturePaths in e2e/acceptance/features.go, so the default suite -- and
  # with it the e2e run -- never picks it up: the scenario reconfigures the block topology while it
  # runs. Run it explicitly against a cluster that declares one:
  #
  #   go run ./e2e/cmd/acceptance --kubectl-context <ctx> --scenario features/topology_block.feature
  #
  # It skips itself on a cluster with no resizable block topology, and restores the original
  # blockSizes when it ends, however it ends.
  @gpu @block_topology @soperator_version_>=5.0.0
  Scenario: A block topology schedules jobs after its base size changes
    Given the cluster is configured with a block topology whose base size can be changed
    And the operator published the topology config
    Then Slurm reports the blocks the operator rendered
    And a job runs in a partition of the block topology
    When the block topology base size is halved
    Then the rendered config carries the new block sizes
    And Slurm reports the new base block size
    And the topology JailedConfig has no pending reconfigure request
    And a job runs in a partition of the block topology
