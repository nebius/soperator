Feature: Enroot MPI NCCL transfer
  @gpu
  Scenario: Enroot MPI/NCCL transfer job completes successfully across two GPU workers
    Given a finite Enroot MPI/NCCL transfer job is submitted on two GPU workers
    When the Enroot MPI/NCCL transfer job is running
    Then the Enroot MPI/NCCL transfer job completes successfully
