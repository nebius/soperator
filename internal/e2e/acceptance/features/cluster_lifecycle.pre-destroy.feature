Feature: Cluster lifecycle acceptance flow
  To validate the first acceptance-test PoC
  As the soperator team
  We run a single stateful flow on one freshly created cluster

  Scenario: Exercise the core acceptance flow on a provisioned cluster
    Given the provisioned Slurm cluster is reachable
    When a regular user can SSH from the login node to a worker without extra SSH options
    And packages can be installed on the worker without breaking the NVIDIA driver
    Then a maintenance event replaces the worker node and returns it to service
