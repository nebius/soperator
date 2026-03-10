Feature: Package installation
  To validate node usability after provisioning
  As the soperator team
  We verify packages can be installed on a worker node

  Scenario: Installing jq does not break the NVIDIA driver
    Given the provisioned Slurm cluster is reachable
    When packages can be installed on the worker without breaking the NVIDIA driver
