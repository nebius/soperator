Feature: Enroot containers
  @gpu
  Scenario: Enroot and Pyxis cache images and clean up runtime data
    Given Enroot direct SquashFS startup is enabled
    Given a long-running Enroot NCCL job is submitted on two GPU workers
    When the Enroot NCCL job is running
    Then Enroot cache is populated on local storage on a worker
    And Enroot squashfs image is present on a worker
    And Enroot squashfs image is mounted directly while the job is running
    And the Enroot NCCL job is still running
    When the Enroot NCCL job is cancelled
    Then Enroot direct runtime is cleaned up and squashfs cache remains
    When the same Enroot NCCL job is submitted again
    Then Enroot squashfs artifact is reused without materializing runtime data
    And the repeated Enroot NCCL job is still running
    When the repeated Enroot NCCL job is cancelled
    When a named Enroot container job is submitted
    Then the named Enroot runtime directory remains after cancellation
    When the named Enroot runtime directory is cleaned up
    Then the named Enroot runtime directory is removed
