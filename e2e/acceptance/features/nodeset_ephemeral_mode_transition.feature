Feature: NodeSet ephemeral mode transitions
  # Sequential: Changes NodeSet lifecycle mode and checks every worker Pod UID without whole-set ownership.
  @soperator_version_>=5.0.0 @sequential
  Scenario: A static NodeSet can transition to ephemeral mode and back
    Given a ready static NodeSet is selected for a mode transition
    When the selected NodeSet is switched to ephemeral mode
    Then all requested workers remain active without pod recreation
    When the selected NodeSet is switched back to static mode
    Then its power state is removed and all static workers are ready
