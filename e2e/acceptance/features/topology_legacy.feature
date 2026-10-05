Feature: Slurm network topology before named topologies
  # Clusters older than 5.0.0 describe their fabric with a single topology/tree configuration
  # instead of the named topologies of topology.yaml, so this scenario is bounded to those
  # versions. features/topology.feature covers 5.0.0 and later.
  @soperator_version_>=4.0.0,<5.0.0
  Scenario: scontrol topology and SLURM_TOPOLOGY_ADDR agree across workers
    Given the Slurm topology plugin is topology/tree
    When scontrol show topology is parsed into a switch tree
    Then every worker in the main partition is present in the topology
    When a job runs on all available workers and reports SLURM_TOPOLOGY_ADDR
    Then each task's SLURM_TOPOLOGY_ADDR matches its position in the topology
