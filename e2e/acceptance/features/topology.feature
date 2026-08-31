Feature: Slurm named topologies

  Background:
    Given the cluster is configured with several named topologies

  @multi_topology @soperator_version_>=5.0.0
  Scenario: The operator publishes and Slurm loads the configured topologies
    Given the operator published the topology config
    Then the config declares several named topologies
    And exactly one of them is the cluster default
    And one of them is a flat topology
    And the topology config is delivered to the jail
    When Slurm is asked which topologies it loaded
    Then Slurm loaded exactly the topologies the operator rendered

  @multi_topology @soperator_version_>=5.0.0
  Scenario: Partitions and workers use their configured topologies
    Given the operator published the topology config
    When Slurm is asked which topologies it loaded
    Then every partition is bound to a topology Slurm loaded
    And every GPU worker is listed by a fabric topology
    And no CPU-only worker is listed by a fabric topology
    And every running worker is registered into the topologies that list it

  @multi_topology @soperator_version_>=5.0.0
  Scenario: Every configured topology can schedule a job
    Given the operator published the topology config
    Then a job runs in a partition of every topology
