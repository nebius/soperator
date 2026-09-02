Feature: Slurm accounting

  @soperator_version_>=4.0.0
  Scenario: A user job is recorded and included in Slurm accounting reports
    Given Slurm accounting is reachable and the cluster is registered
    And an acceptance test user exists
    When a test Slurm account is created and associated with the user
    Then the user association is visible in Slurm accounting
    When the user submits a one-node smoke job to the test account
    Then the job completes and is recorded with the expected user and account
    And CPU, billing, cluster utilization, and job-size reports contain accounting data
