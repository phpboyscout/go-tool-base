@controls @integration
Feature: Service Lifecycle Management
  The controller manages services through a 4-state FSM:
  Unknown -> Running -> Stopping -> Stopped

  Background:
    Given a controller with no OS signal handling

  @smoke
  Scenario: Initial state is Unknown
    Then the controller state is "unknown"

  @smoke
  Scenario: Start and stop a single service
    Given a service "worker" is registered
    When the controller starts
    Then the controller state is "running"
    And the service "worker" has been started
    When the controller stops
    Then the controller reaches "stopped" state within 5 seconds
    And the service "worker" has been stopped exactly 1 time

  Scenario: Status message triggers status check
    Given a service "worker" is registered
    When the controller starts
    And a status message is sent
    Then the service "worker" status has been checked at least 1 time
    And the controller state is "running"

  Scenario: Multiple status messages are processed
    Given a service "worker" is registered
    When the controller starts
    And 3 status messages are sent
    Then the service "worker" status has been checked at least 3 times
    And the controller state is "running"

  Scenario: Stop message via message channel
    Given a service "worker" is registered
    When the controller starts
    And a stop message is sent via the message channel
    Then the controller reaches "stopped" state within 5 seconds
    And the service "worker" has been stopped exactly 1 time

  Scenario: Context cancellation triggers shutdown
    Given a service "worker" is registered
    When the controller starts
    And the parent context is cancelled
    Then the controller reaches "stopped" state within 5 seconds
    And the service "worker" has been stopped exactly 1 time

  Scenario: Concurrent stop calls are idempotent
    Given a service "worker" is registered
    When the controller starts
    And 100 goroutines call stop concurrently
    Then the controller reaches "stopped" state within 5 seconds
    And the service "worker" has been stopped exactly 1 time

  Scenario: Stop on already stopped controller is a no-op
    Given a service "worker" is registered
    When the controller starts
    And the controller stops
    And the controller reaches "stopped" state within 5 seconds
    And the controller stops
    Then the controller state is "stopped"

  Scenario: Service start failure is reported
    Given a service "failing" is registered with a start error "startup failed"
    When the controller starts
    Then the logs contain "startup failed" within 2 seconds
