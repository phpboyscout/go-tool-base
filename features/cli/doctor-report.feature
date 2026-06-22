@cli @doctor
Feature: CLI Doctor Report Command
  The `doctor report` subcommand prints a single, redacted, paste-ready support
  bundle — versions, resolved config, paths, feature flags, and the doctor
  report — that is safe to paste into a public issue. It is gated by the
  default-on DoctorCmd feature.

  Background:
    Given the gtb binary is built

  @smoke
  Scenario: doctor report redacts a literal API key in text output
    Given a temporary directory with a config file:
      """
      log:
        level: info
      anthropic:
        api:
          key: sk-ant-SUPERSECRETVALUE123
      """
    When I run gtb with "doctor report"
    Then the exit code is 0
    And stdout contains "Config (redacted):"
    And stdout contains "<redacted>"
    And stdout does not contain "sk-ant-SUPERSECRETVALUE123"

  Scenario: doctor report JSON output is valid and redacted
    Given a temporary directory with a config file:
      """
      anthropic:
        api:
          key: sk-ant-SUPERSECRETVALUE123
      """
    When I run gtb with "doctor report --output json"
    Then the exit code is 0
    And stdout is valid JSON
    And the JSON field "command" equals "doctor report"
    And stdout does not contain "sk-ant-SUPERSECRETVALUE123"
