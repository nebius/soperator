Feature: Tailscale login sidecars
  @soperator_version_>=5.0.0
  Scenario: Tailscale login sidecars wait for authentication and persist their state
    Given Tailscale is enabled on login pods
    When the Tailscale login pods are discovered from the live cluster
    Then every Tailscale sidecar is waiting for authentication
    And every login pod has a persisted Tailscale state Secret
    When one Tailscale login pod is restarted
    Then the restarted Tailscale sidecar is waiting for authentication
    And the restarted login pod reuses its Tailscale state Secret
