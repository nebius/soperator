Feature: Tailscale login access
  @soperator_version_>=4.0.0
  Scenario: Every login pod offers a Tailscale authentication URL
    Given Tailscale is configured for the login workload
    Then every login pod reports a Tailscale authentication URL
