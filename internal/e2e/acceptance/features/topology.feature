Feature: Slurm network topology
  Scenario: scontrol topology and SLURM_TOPOLOGY_ADDR agree across workers
    Given the Slurm topology plugin is topology/tree
    When scontrol show topology is parsed into a switch tree
    Then every worker in the main partition is present in the topology
    When a job runs on all available workers and reports SLURM_TOPOLOGY_ADDR
    Then each task's SLURM_TOPOLOGY_ADDR matches its position in the topology
