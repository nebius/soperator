Feature: Slurm tree-topology scheduling

  # Remove @unstable after worker topology registration is fixed.
  @gpu @tree_topology @unstable @soperator_version_>=5.0.0
  Scenario: A switch limit rejects a cross-leaf allocation
    Given the cluster is configured with a tree topology spanning multiple leaf switches
    And the operator published the topology config
    And Slurm loaded the tree topology
    When two configured workers on different leaf switches are requested without a switch limit
    Then the cross-leaf allocation succeeds
    When the same workers are requested with at most one switch
    Then the cross-leaf allocation is rejected
    And two workers on the same leaf switch are still allocated within one switch
