Feature: Package installation
  @gpu @soperator_version_>=5.0.0
  Scenario: Installing and uninstalling cowsay preserves GPU tooling
    Given the NVIDIA driver is working on a worker node
    And package "cowsay" is not installed on the worker node
    When package "cowsay" is installed on the worker node
    Then command "/usr/games/cowsay Soperator" succeeds on the worker node
    And the NVIDIA driver is still working on the worker node
    When package "cowsay" is uninstalled from the worker node
    Then package "cowsay" is not installed on the worker node
    And the NVIDIA driver is still working on the worker node

  @soperator_version_>=5.0.0
  Scenario: Installing and uninstalling nvitop preserves GPU tooling
    Given the NVIDIA driver is working on a worker node
    And package "nvitop" is not installed on the worker node
    When package "nvitop" is installed on the worker node
    Then command "nvitop --once" succeeds on the worker node
    And the NVIDIA driver is still working on the worker node
    When package "nvitop" is uninstalled from the worker node
    Then package "nvitop" is not installed on the worker node
    And the NVIDIA driver is still working on the worker node
