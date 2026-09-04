Feature: Login autoscaling
  @soperator_version_>=5.0.0
  Scenario: Login pods and Kubernetes nodes scale up without automatic pod scale-down
    Given the login workload is ready for an autoscaling lifecycle test
    When login autoscaling is enabled with one additional replica of capacity
    Then the login workload remains at its default replica count without pressure
    When CPU pressure is created on the login pods
    Then the login workload scales to its autoscaling maximum
    When CPU pressure is removed from the login pods
    Then the login workload does not scale down automatically
    When the fixed login size is changed while autoscaling is enabled
    Then the autoscaled login pods are preserved without recreation
    When login autoscaling is disabled
    Then the login workload scales down to the fixed size
