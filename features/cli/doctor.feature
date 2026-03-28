@cli @smoke
Feature: CLI Doctor Command
  The doctor command runs diagnostic checks against the local environment
  and reports their status.

  Background:
    Given the gtb binary is built

  Scenario: Text output shows diagnostic checks
    When I run gtb with "doctor"
    Then the exit code is 0
    And stdout contains "Go version"
    And stdout contains "Configuration"

  Scenario: JSON output returns structured report
    When I run gtb with "doctor --output json"
    Then the exit code is 0
    And stdout is valid JSON
    And the JSON field "status" equals "success"
    And the JSON field "command" equals "doctor"
    And the JSON field "data.tool" is not empty
    And the JSON field "data.checks" is an array with at least 3 items
