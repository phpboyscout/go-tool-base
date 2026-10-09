@cli @smoke @signal
Feature: Signal-aware execution context
  An external SIGINT, SIGTERM or SIGHUP cancels the running command's context so
  it can unwind gracefully, and once it has, the process dies by that signal: a
  shell sees 128+signum and a process manager sees a clean stop by the signal. A
  SIGINT the process started with ignored stays ignored. This exercises real OS
  signal delivery and process exit, which the deterministic unit tests cannot
  cover.

  Background:
    Given the gtb binary is built

  Scenario Outline: A signal cancels the command context and the run ends by that signal after its drain
    Given the gtb binary is running the "block" command
    When I send <signal> to the running gtb process
    Then the gtb process is terminated by <signal>
    And the running process stdout contains "graceful shutdown complete"

    Examples:
      | signal  |
      | SIGINT  |
      | SIGTERM |
      | SIGHUP  |

  Scenario: A SIGINT ignored at start stays ignored
    Given the gtb binary is running the "block" command with SIGINT ignored
    When I send SIGINT to the running gtb process
    Then the gtb process is still running
    When I send SIGTERM to the running gtb process
    Then the gtb process is terminated by SIGTERM
    And the running process stdout contains "graceful shutdown complete"
