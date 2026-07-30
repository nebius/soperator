Feature: Docker containers
  Scenario: Docker container lifecycle uses local storage
    Given a long-running Docker container job is submitted on two workers
    When the Docker container job is running
    Then Docker image and runtime storage is populated on a worker
    And Docker containers from the job are running on selected workers
    When the Docker container job is cancelled
    And Docker containers from the job are stopped explicitly
    Then Docker containers from the job are no longer running

  @gpu
  Scenario: Docker containers can access GPUs
    Given a Docker GPU smoke job is submitted on one GPU worker
    Then the Docker GPU smoke job succeeds and reports visible GPUs
