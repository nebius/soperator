Feature: NCCL

  @essential @gpu @soperator_version_>=5.0.0
  Scenario: Multi-node NCCL all-reduce succeeds
    Given two idle GPU workers are available for NCCL validation
    When the jail NCCL utility runs a bounded all-reduce test on them
    Then the NCCL result is correct
