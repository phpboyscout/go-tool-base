@cli @controls
Feature: A supervised tool's exit code reports how its services ended
  A tool built on go-tool-base runs long-lived services under a go/controls
  controller inside a command. When a service fails for good, the process must
  exit non-zero, so a supervisor such as systemd or Kubernetes restarts it. When
  the work completes, it exits zero. The command reads the controller's stop
  cause and returns an error only for a give-up; the root owns the exit code.

  Background:
    Given the gtb binary is built

  Scenario: A service that fails for good exits the tool non-zero
    When I run gtb with "supervise --give-up"
    Then the exit code is 1
    And stdout contains "supervisor stopped: cause=gave_up"
    And stderr contains "service failing gave up"

  Scenario: A service that finishes its work exits the tool cleanly
    When I run gtb with "supervise --complete"
    Then the exit code is 0
    And stdout contains "supervisor stopped: cause=completed"
