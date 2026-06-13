@cli @smoke @signal
Feature: Signal-aware execution context
  An external SIGINT cancels the running command's context so it can unwind
  gracefully, and the process exits with the conventional 128+signum code
  (130 for SIGINT). This exercises real OS signal delivery and process exit,
  which the deterministic unit tests cannot cover.

  Background:
    Given the gtb binary is built

  Scenario: SIGINT cancels the command context and exits 130 after graceful shutdown
    Given the gtb binary is running the "block" command
    When I send SIGINT to the running gtb process
    Then the gtb process exits with code 130
    And the running process stdout contains "graceful shutdown complete"
