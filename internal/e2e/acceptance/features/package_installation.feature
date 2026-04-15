Feature: Package installation
  @gpu
  Scenario: Installing nvitop does not break the NVIDIA driver
    Given the NVIDIA driver is working on a worker node
    When nvitop is installed on the worker node
    Then the NVIDIA driver is still working on the worker node
    And nvitop is available on the worker node
