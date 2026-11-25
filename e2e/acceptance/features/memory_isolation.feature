Feature: Job memory isolation
  # Sequential: Checks host-wide OOM logs and service identities without owning every worker on the host.
  @heavy @soperator_version_>=5.0.0 @sequential
  Scenario Outline: Job memory exhaustion leaves neighboring workloads healthy
    Given two memory-limited jobs run on the same worker using <execution>
    When one job allocates memory below its limit
    Then that allocation succeeds within its job memory limit
    When the job allocates memory beyond its limit
    Then an OOM kill is contained within that job
    And the neighboring job retains its memory and keeps running
    And worker and host services remain healthy

    Examples:
      | execution |
      | native    |
      | Enroot    |
      | Docker    |
