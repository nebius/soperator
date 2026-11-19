Feature: NCCL debug logging

  @gpu @soperator_version_>=5.0.0
  Scenario: Non-root NCCL jobs produce diagnostics and release logging resources
    Given an idle GPU worker is available for NCCL debug logging
    And the soperatorchecks user is available for NCCL debug jobs
    When the soperatorchecks user runs short full-node NCCL jobs with INFO and WARN logging
    Then both NCCL debug jobs report correct results
    And their NCCL diagnostic logs reflect the requested verbosity
    And temporary NCCL logging resources are released after each step

  @soperator_version_>=5.0.0
  Scenario: Concurrent container starts retain NCCL diagnostics
    Given a worker is selected for concurrent container logging
    When 4 background jobs repeatedly run short native steps with NCCL logging
    And 4 batch jobs each start 25 Ubuntu containers with NCCL logging on the same worker
    And the background jobs are stopped after all container attempts finish
    Then all 100 container starts succeed
    And every container's log marker is readable outside the container
    And the background jobs report no failed steps
