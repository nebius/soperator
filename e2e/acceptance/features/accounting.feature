Feature: Slurm accounting

  @soperator_version_>=4.0.0
  Scenario: A user job is recorded with its allocated resources
    Given Slurm accounting is reachable and the cluster is registered
    And an acceptance test user exists
    When a test Slurm account is created and associated with the user
    Then the user association is visible in Slurm accounting
    When the user submits a one-node smoke job to the test account
    # sreport reads hourly slurmdbd rollups, so immediate job usage is verified through sacct.
    Then the job completes and is recorded with the expected user, account, and allocated resources
  # Sequential: Changes shared SlurmDBD privacy settings and restarts accounting.
  @heavy @sequential @soperator_version_>=5.0.0
  Scenario: Accounting privacy preserves access to a Linux user's own records
    # Coverage: jobs and users via sacct and sacctmgr, before and after restart.
    # Deferred: usage privacy via sreport, including own usage and hidden foreign usage.
    # This scenario does not wait for hourly usage rollups.
    Given Slurm accounting is reachable and the cluster is registered
    And independent Linux users and accounts are ready for accounting privacy checks
    When the users complete jobs with different working directories and submit commands
    Then the test jobs are visible while accounting privacy is disabled
    When accounting privacy is enabled for jobs, usage, and users
    Then accounting records follow user and account permissions
    When the accounting service is restarted
    Then accounting records follow user and account permissions
    When accounting privacy is disabled again
    Then the test jobs are visible while accounting privacy is disabled
    And the original accounting configuration and privacy fixtures are restored
