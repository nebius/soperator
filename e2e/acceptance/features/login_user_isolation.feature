Feature: Login user isolation
  @heavy @soperator_version_>=5.0.0
  Scenario: A login user exceeding the memory limit is OOM-killed without affecting other users or login services
    Given login user isolation is configured with explicit per-user memory limits
    And two regular users have SSH sessions on the same login node
    Then both users' SSH sessions are limited by the explicit memory limits
    When the second user holds memory in a background process on the login node
    And the first user allocates memory above the per-user throttling threshold
    Then the first user's allocation is throttled but not killed
    When the first user allocates memory beyond the per-user hard limit
    Then the first user's allocation is OOM-killed inside the first user's cgroup
    And the second user's background process keeps running with its memory intact
    And the second user can still SSH to the login node, allocate memory and run Slurm commands
    And login pods and services were not affected by the memory exhaustion
