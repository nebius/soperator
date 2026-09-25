Feature: Slurm block-topology scheduling
  # Tagged @unstable, so the default suite skips it: it needs a cluster with a block topology, and one
  # scenario reconfigures that topology while it runs. Run it with --run-unstable, alone or with the
  # rest of the unstable scenarios:
  #
  #   go run ./e2e/cmd/acceptance --kubectl-context <ctx> --run-unstable --scenario features/topology_block.feature
  #
  # Each scenario skips itself on a cluster without the block topology it needs. The resize scenario
  # restores the original blockSizes when it ends, however it ends.
  @gpu @block_topology @unstable @soperator_version_>=5.0.0
  Scenario: A job assigns task ranks in rendered block order
    Given the cluster is configured with a topology spanning multiple blocks
    And the operator published the topology config
    When Slurm is asked which topologies it loaded
    Then Slurm loaded exactly the topologies the operator rendered
    And a multi-block job assigns SLURM_PROCID in rendered block order

  @gpu @block_topology @unstable @soperator_version_>=5.0.0
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
