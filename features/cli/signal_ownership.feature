@cli @signal @controls
Feature: Single-owner signal handling
  The root command is the sole owner of SIGINT/SIGTERM. A service supervisor
  running underneath it observes the cancellation of cmd.Context() rather than
  registering a competing handler, so one interrupt produces exactly one
  shutdown.

  signal.Notify is additive, so a second handler would not fail loudly — it
  would race the first, and the cause a service observes would depend on
  goroutine scheduling. Only a real OS signal delivered to a process running
  both layers can catch that, which is why this scenario exists alongside the
  deterministic unit tests.

  Background:
    Given the gtb binary is built

  Scenario: One SIGINT drives exactly one shutdown through the supervisor
    Given the gtb binary is running the "supervise" command
    When I send SIGINT to the running gtb process
    Then the gtb process exits with code 130
    And the running process stdout contains "service stopped: cause=ErrShutdown"
    And the running process stdout contains "supervised shutdown complete"
    And the running process output contains "received signal" exactly 1 time(s)
