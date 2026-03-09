Feature: Internal SSH
  To validate internal user workflows
  As the soperator team
  We verify a regular user can reach worker nodes from the login node

  Scenario: A regular user can SSH to a worker without extra options
    Given the provisioned Slurm cluster is reachable
    When a regular user can SSH from the login node to a worker without extra SSH options
