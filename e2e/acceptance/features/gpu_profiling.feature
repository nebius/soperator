Feature: GPU profiling

  Background:
    Given a GPU worker is available for profiling
    And the soperatorchecks user is available on the worker
    And GPU profiling is enabled for non-root users on the worker

  @gpu @soperator_version_>=4.0.0
  Scenario: A non-root user can profile a GPU workload with Nsight Compute
    When the soperatorchecks user submits an Nsight Compute profiling job
    Then the Nsight Compute profiling job succeeds

  @gpu @soperator_version_>=4.0.0
  Scenario: A non-root user can profile a GPU workload with Nsight Systems
    When the soperatorchecks user submits a full-node Nsight Systems profiling job
    Then the Nsight Systems profiling job succeeds and produces a report
