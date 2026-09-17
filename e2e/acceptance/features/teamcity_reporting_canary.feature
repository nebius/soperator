Feature: TeamCity native reporting canary

  @soperator_version_>=4.0.0
  Scenario: TeamCity canary passes
    Then the TeamCity reporting canary passes

  @skip @soperator_version_>=4.0.0
  Scenario: TeamCity canary skips
    Then the TeamCity reporting canary passes

  @soperator_version_>=4.0.0
  Scenario: TeamCity canary fails
    Then the TeamCity reporting canary fails
