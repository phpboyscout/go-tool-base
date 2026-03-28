@controls @integration @slow
Feature: Graceful Shutdown
  When the controller receives SIGINT, it must drain in-flight
  requests before stopping. These scenarios require real HTTP and
  gRPC servers bound to free ports.

  Scenario: SIGINT triggers clean shutdown with HTTP and gRPC servers
    Given a controller with OS signal handling
    And an HTTP server registered on a free port
    And a gRPC server registered on a free port
    When the controller starts
    And the HTTP server is healthy
    And the gRPC server is healthy
    And the controller receives SIGINT
    Then the controller reaches "stopped" state within 10 seconds
    And the logs do not contain "server shutdown failed"

  Scenario: In-flight HTTP requests complete during shutdown
    Given a controller with OS signal handling and 5 second shutdown timeout
    And an HTTP server registered on a free port
    And a gRPC server registered on a free port
    And the HTTP server has a slow handler "/api/slow" that takes 2 seconds
    When the controller starts
    And the HTTP server is healthy
    And a client sends a GET request to the slow handler
    And the request is in-flight
    And the controller receives SIGINT
    Then the controller reaches "stopped" state within 10 seconds
    And the in-flight request completed successfully

  Scenario: SIGINT during startup still shuts down cleanly
    Given a controller with OS signal handling
    And an HTTP server registered on a free port
    And a gRPC server registered on a free port
    And a service "slow-init" that takes 500ms to start
    When the controller starts in the background
    And the "slow-init" service has begun starting
    And the controller receives SIGINT
    Then the controller reaches "stopped" state within 10 seconds
    And the logs contain "Stopping Services"
