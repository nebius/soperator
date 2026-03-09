Feature: Cluster creation
  To start the acceptance flow from a known-good environment
  As the soperator team
  We verify the provisioned Slurm cluster is reachable

  Scenario: The provisioned cluster is ready for acceptance tests
    Given the provisioned Slurm cluster is reachable
