@controls @integration
Feature: A GTB tool registers its transports from config
  The controller, health monitoring, graceful shutdown and rate limiting are
  go/controls and go/transport behaviour, tested where they live. What GTB
  owns is the adapter: pkg/http and pkg/grpc read a config section and
  register a transport with the controller, passing any server option through.
  These scenarios prove that path and nothing the modules already prove.

  Decision: https://gitlab.com/phpboyscout/go-tool-base/-/work_items/54

  @slow
  Scenario: HTTP and gRPC servers registered from config come up healthy and stop on SIGINT
    Given a controller with OS signal handling
    And an HTTP server registered on a free port
    And a gRPC server registered on a free port
    When the controller starts
    And the HTTP server is healthy
    And the gRPC server is healthy
    And the controller receives SIGINT
    Then the controller reaches "stopped" state within 10 seconds
    And the logs do not contain "server shutdown failed"

  @smoke
  Scenario: A server option given to the HTTP adapter reaches the server
    Given a controller with no OS signal handling
    And an HTTP server with a 2 rps rate limiter
    When the controller starts
    And the HTTP server is healthy
    And 5 rapid GET requests are sent to "/"
    Then 2 of the requests succeed with status 200
    And 3 of the requests are rejected with status 429
