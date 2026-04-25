Feature: Docker containers
  @gpu
  Scenario: A long-running Docker NCCL job uses local storage and cleans up containers
    Given a long-running Docker NCCL job is submitted on two GPU workers
    When the Docker NCCL job is running
    Then Docker overlayfs storage is populated on a worker
    And Docker container content blobs are populated on a worker
    And a Docker container from the job is running on workers
    And the Docker NCCL job is still running
    When the Docker NCCL job is cancelled
    Then Docker containers from that job are no longer running
