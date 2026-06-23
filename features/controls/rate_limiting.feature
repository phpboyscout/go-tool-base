@controls @integration
Feature: Server-side rate limiting
  A GTB HTTP server can opt into the token-bucket RateLimitMiddleware to shed
  excess load. The limiter admits up to the burst capacity and rejects the rest
  with 429 Too Many Requests. Health probes are mounted outside the middleware
  chain, so a global limiter never throttles liveness/readiness.

  Background:
    Given a controller with no OS signal handling

  @smoke
  Scenario: A 2 rps limiter rejects a five-request burst
    Given an HTTP server with a 2 rps rate limiter
    When the controller starts
    And the HTTP server is healthy
    And 5 rapid GET requests are sent to "/"
    Then 2 of the requests succeed with status 200
    And 3 of the requests are rejected with status 429
