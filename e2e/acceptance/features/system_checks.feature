Feature: System checks
  @soperator_version_>=5.0.0
  Scenario: Worker pod ephemeral storage pressure drains and recovers a Slurm node
    Given a healthy worker pod is selected
    When pod-local ephemeral storage is filled above the warning threshold
    Then the selected worker is drained by pod_ephemeral_storage
    When the pod-local ephemeral storage fill file is removed
    Then the selected worker no longer has pod_ephemeral_storage reason
    And the selected worker is usable after pod_ephemeral_storage

  # TODO: Make soperatorchecks --not-ready-timeout configurable through Helm and
  # set a lower value for e2e clusters, then remove @unstable from this scenario.
  @unstable @soperator_version_>=5.0.0
  Scenario: Non-responding kubelet recreates a Kubernetes node and recovers the worker
    Given a healthy worker pod is selected
    When kubelet is stopped on the selected worker Kubernetes node
    Then the selected worker Kubernetes node is recreated
    And the selected worker pod is recreated and ready
    And the selected Slurm worker is present after kubelet replacement
    And the selected Slurm worker is usable after kubelet replacement
