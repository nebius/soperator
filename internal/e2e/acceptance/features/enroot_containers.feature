Feature: Enroot containers
  @gpu
  Scenario: Enroot and Pyxis cache images and clean up runtime data
    Given a long-running Enroot NCCL job is submitted on two GPU workers
    When the Enroot NCCL job is running
    Then Enroot cache is populated on local storage on a worker
    And Enroot squashfs image is present on a worker
    And Enroot runtime container data is visible while the job is running
    And the Enroot NCCL job is still running
    When the Enroot NCCL job is cancelled
    Then Enroot runtime data is cleaned up and squashfs cache remains
    When the same Enroot NCCL job is submitted again
    Then Enroot runtime data is repopulated without changing the squashfs artifact
    And the repeated Enroot NCCL job is still running
    When the repeated Enroot NCCL job is cancelled
    When a named Enroot container job is submitted
    Then the named Enroot runtime directory remains after cancellation
    When the named Enroot runtime directory is cleaned up
    Then the named Enroot runtime directory is removed
