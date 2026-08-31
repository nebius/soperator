Feature: Enroot containers
  @soperator_version_>=5.0.0
  Scenario Outline: Enroot containers can run over SSH
    Given an Enroot SSH test user exists
    When the user imports an Enroot image over SSH on a <node> node
    And the user starts the imported Enroot image over SSH
    Then the container reports Ubuntu

    Examples:
      | node   |
      | login  |
      | worker |

  @soperator_version_>=4.0.0
  Scenario: Enroot and Pyxis cache images and clean up runtime state
    Given a long-running Enroot container job is submitted on two workers
    When the Enroot container job is running
    Then an Enroot image artifact is present on a worker
    And Enroot runtime state is visible while the job is running
    When the Enroot container job is cancelled
    Then Enroot runtime state is cleaned up and the image artifact remains
    When the same Enroot container job is submitted again
    Then the existing Enroot image artifact is reused
    When the repeated Enroot container job is cancelled
    Then Enroot runtime state is cleaned up

  @gpu @soperator_version_>=5.0.0
  Scenario: Enroot containers can access GPUs
    Given an Enroot GPU smoke job is submitted on one GPU worker
    Then the Enroot GPU smoke job succeeds and reports visible GPUs
