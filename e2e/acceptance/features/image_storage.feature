Feature: Node-local image storage
  @heavy @soperator_version_>=5.0.0
  Scenario: A worker restarts with user content on image storage and containers keep working for other users
    Given a healthy worker with node-local image storage is acquired
    And two container test users exist
    And the first user has imported an Enroot image and run a Docker container on the worker
    And a Docker container of the first user holds a directory tree longer than the path limit
    When the worker pod is restarted
    Then the worker returns to service
    And the Docker image and the deep directory tree are preserved with their permissions
    And the second user runs the same Enroot image and the same Docker image on the worker
