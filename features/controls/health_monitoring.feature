@controls @integration
Feature: Health Monitoring
  The controller supports registering health checks that report status
  via readiness, liveness, and overall health endpoints. Checks can be
  synchronous (run inline per request) or asynchronous (cached on interval).

  Background:
    Given a controller with no OS signal handling

  @smoke
  Scenario: Healthy readiness check reports OK
    Given a health check "db" of type "readiness" that returns healthy
    Then the readiness report is overall healthy
    And the readiness report includes "db" with status "OK"

  @smoke
  Scenario: Unhealthy readiness check makes report unhealthy
    Given a health check "db" of type "readiness" that returns unhealthy with "connection refused"
    Then the readiness report is not overall healthy
    And the readiness report includes "db" with status "ERROR"

  Scenario: Degraded check keeps report healthy
    Given a health check "cache" of type "readiness" that returns degraded with "high latency"
    Then the readiness report is overall healthy
    And the readiness report includes "cache" with status "DEGRADED"

  Scenario: Liveness check appears only in liveness report
    Given a health check "heartbeat" of type "liveness" that returns healthy
    Then the liveness report includes "heartbeat" with status "OK"
    And the readiness report does not include "heartbeat"

  Scenario: Readiness check appears only in readiness report
    Given a health check "db" of type "readiness" that returns healthy
    Then the readiness report includes "db" with status "OK"
    And the liveness report does not include "db"

  Scenario: Check of type "both" appears in all reports
    Given a health check "core" of type "both" that returns healthy
    Then the readiness report includes "core" with status "OK"
    And the liveness report includes "core" with status "OK"

  Scenario: Registration after start fails
    Given a service "worker" is registered
    When the controller starts
    Then registering a health check "late" fails with "after start"

  Scenario: Duplicate check name fails
    Given a health check "db" of type "readiness" that returns healthy
    Then registering a health check "db" fails with "duplicate"

  Scenario: Async check caches result
    Given an async health check "cache" with interval 100ms that returns healthy
    When the controller starts
    And I wait 200ms for the async check to run
    Then querying readiness 3 times returns cached results
    And the async check "cache" ran at most 5 times

  @slow
  Scenario: Service restarts after health failure threshold
    Given a service "flaky" that starts successfully and becomes unhealthy after 1 status check
    And the service "flaky" has a restart policy with threshold 2 and interval 10ms
    When the controller starts
    Then the service "flaky" restarts at least 2 times within 5 seconds
