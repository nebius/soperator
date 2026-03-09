Feature: Node replacement
  To validate maintenance handling
  As the soperator team
  We verify a maintenance event replaces a worker and returns it to service

  Scenario: A maintenance event replaces the selected worker node
    Given the provisioned Slurm cluster is reachable
    Then a maintenance event replaces the worker node and returns it to service
