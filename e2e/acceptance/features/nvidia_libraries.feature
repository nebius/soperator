Feature: NVIDIA driver libraries

  # Sequential: Selects GPU workers from different NodeSets from a snapshot without reserving them.
  @gpu @soperator_version_>=5.0.0 @sequential
  Scenario: Native jobs see every NVIDIA driver library mounted into worker pods
    Given healthy GPU workers are selected from every GPU NodeSet for NVIDIA library checks
    And their slurmd containers are configured with the required NVIDIA driver capabilities
    When the NVIDIA driver libraries mounted into the worker pods are listed
    Then every worker pod lists the same NVIDIA driver libraries
    When a native job on the selected workers reports its view of the listed libraries
    Then the job sees every listed library as the pod's file with a soname in the linker cache

  # Sequential: Selects GPU workers from different NodeSets from a snapshot without reserving them.
  @gpu @soperator_version_>=5.0.0 @sequential
  Scenario: Enroot jobs see every NVIDIA driver library mounted into worker pods
    Given healthy GPU workers are selected from every GPU NodeSet for NVIDIA library checks
    And their slurmd containers are configured with the required NVIDIA driver capabilities
    When the NVIDIA driver libraries mounted into the worker pods are listed
    Then every worker pod lists the same NVIDIA driver libraries
    When an Enroot job on the selected workers reports its view of the listed libraries
    Then the job sees every listed library as the pod's file with a soname in the linker cache

  # Sequential: Selects GPU workers from different NodeSets from a snapshot without reserving them.
  @gpu @soperator_version_>=5.0.0 @sequential
  Scenario: Docker jobs see every NVIDIA driver library mounted into worker pods
    Given healthy Docker-enabled GPU workers are selected from every Docker-enabled GPU NodeSet for NVIDIA library checks
    And their slurmd containers are configured with the required NVIDIA driver capabilities
    When the NVIDIA driver libraries mounted into the worker pods are listed
    Then every worker pod lists the same NVIDIA driver libraries
    When a Docker job on the selected workers reports its view of the listed libraries
    Then the job sees every listed library as the pod's file with a soname in the linker cache
