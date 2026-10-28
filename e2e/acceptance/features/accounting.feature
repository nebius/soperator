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
