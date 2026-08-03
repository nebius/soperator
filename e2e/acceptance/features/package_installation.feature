Feature: Package installation
  @gpu @soperator_version_>=4.2.0
  Scenario: Installing jq does not break the NVIDIA driver
    Given the NVIDIA driver is working on a worker node
    When jq is installed on the worker node
    Then the NVIDIA driver is still working on the worker node
    And jq is available on the worker node
