Feature: Login autoscaling
  @heavy @soperator_version_>=5.0.0
  Scenario: Login pods scale up without automatic pod scale-down
    Given login autoscaling was enabled during cluster provisioning
    Then the login workload remains at its minimum replica count without pressure
    When CPU pressure is created on the login pods
    Then the login workload scales to its autoscaling maximum
    When CPU pressure is removed from the login pods
    Then the login workload does not scale down automatically
    When the fixed login size is changed through Helm values
    Then the autoscaled login pods are preserved without recreation
    When login autoscaling is disabled through Helm values
    Then the login workload scales down to the fixed size
