Feature: System checks
  Scenario: Worker pod ephemeral storage pressure drains and recovers a Slurm node
    Given a healthy worker pod is selected for ephemeral storage validation
    When pod-local ephemeral storage is filled above the warning threshold
    Then the selected worker is drained by pod_ephemeral_storage
    When the pod-local ephemeral storage fill file is removed
    Then the selected worker recovers from pod_ephemeral_storage

  # TODO: Make soperatorchecks --not-ready-timeout configurable through Helm and
  # set a lower value for e2e clusters, then remove @unstable from this scenario.
  @unstable
  Scenario: Non-responding kubelet recreates a Kubernetes node and recovers the worker
    Given a healthy worker pod is selected for kubelet validation
    When kubelet is stopped on the selected worker Kubernetes node
    Then the selected worker Kubernetes node is recreated
    And the selected worker pod is recreated and ready
    And the selected Slurm worker recovers after kubelet replacement
