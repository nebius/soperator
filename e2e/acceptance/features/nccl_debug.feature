Feature: NCCL debug logging

  @gpu @soperator_version_>=5.0.0
  Scenario: Non-root NCCL jobs produce diagnostics and release logging resources
    Given an idle GPU worker is available for NCCL debug logging
    And the soperatorchecks user is available for NCCL debug jobs
    When the soperatorchecks user runs short full-node NCCL jobs with INFO and WARN logging
    Then both NCCL debug jobs report correct results
    And their NCCL diagnostic logs reflect the requested verbosity
    And temporary NCCL logging resources are released after each step
