Feature: Docker containers
  @soperator_version_>=5.0.0
  Scenario: Docker containers can run over login SSH
    Given login Docker is enabled
    And a Docker SSH test user exists
    When the user runs Docker lifecycle commands over SSH on the login node
    Then Docker uses the login proxy, image storage, and the user's cgroup

  @soperator_version_>=5.0.0
  Scenario Outline: Docker containers can run over SSH on <worker_type> workers
    Given a Docker SSH test user exists
    When the user runs a Docker container over SSH on a <worker_type> worker node
    Then the Docker container succeeds over worker SSH

    Examples:
      | worker_type |
      | CPU         |
      | GPU         |

  @soperator_version_>=4.0.0,<5.0.0
  Scenario Outline: Docker container lifecycle on <worker_type> workers uses local storage
    Given a long-running Docker container job is submitted on two <worker_type> workers
    When the Docker container job is running
    Then Docker image and runtime storage is populated on a worker
    And Docker containers from the job are running on selected workers
    When the Docker container job is cancelled
    And Docker containers from the job are stopped explicitly
    Then Docker containers from the job are no longer running

    Examples:
      | worker_type |
      | CPU         |
      | GPU         |

  @soperator_version_>=5.0.0
  Scenario Outline: Docker container lifecycle on <worker_type> workers uses local storage and the Slurm job cgroup
    Given a long-running Docker container job is submitted on two <worker_type> workers
    When the Docker container job is running
    Then Docker image and runtime storage is populated on a worker
    And a Docker container from the job uses the Slurm job cgroup
    And Docker containers from the job are running on selected workers
    When the Docker container job is cancelled
    And Docker containers from the job are stopped explicitly
    Then Docker containers from the job are no longer running

    Examples:
      | worker_type |
      | CPU         |
      | GPU         |

  @gpu @soperator_version_>=5.0.0
  Scenario: Docker containers can access GPUs
    Given a Docker GPU smoke job is submitted on one GPU worker
    Then the Docker GPU smoke job succeeds and reports visible GPUs
