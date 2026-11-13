Feature: Worker VM stop and start

  @heavy @soperator_version_>=5.0.0
  Scenario: Workers recover after their VMs are stopped and started
    # The whole-node reservation keeps periodic and other jobs off the workers under test.
    Given up to 2 workers of every NodeSet are reserved for a VM stop-start
    And the reserved workers are idle
    # A drain set by hand must survive the stop-start: it must not silently return
    # a worker to service that an administrator took out of it.
    And an administrator drains one reserved worker

    When the VMs of the reserved workers are stopped
    Then Slurm reports the reserved workers as not responding
    And the Slurm controller answers cluster queries

    When the VMs of the reserved workers are started
    Then the reserved workers return to service without manual intervention
    And the drained worker keeps its drain

    When the administrator resumes the drained worker
    Then the reserved workers are idle
    # A started VM may land on another host under other switches; the controller must still place
    # the worker in the same topologies as before, not under an unknown switch or block.
    And the reserved workers rejoin their topologies
    And a job runs across all reserved workers
    And a job sees every GPU of each reserved GPU worker
    And node-local scratch is mounted on a dedicated filesystem on every reserved worker that has it
