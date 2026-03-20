Feature: Cluster creation
  Scenario: The provisioned cluster is ready for acceptance tests
    Then all Slurm pods are running in the cluster
