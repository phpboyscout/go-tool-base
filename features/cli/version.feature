@cli @smoke
Feature: CLI Version Command
  The version command displays build information in text or JSON format.

  Background:
    Given the gtb binary is built

  Scenario: Text output shows version fields
    When I run gtb with "version"
    Then the exit code is 0
    And stdout contains "Version:"
    And stdout contains "Build:"
    And stdout contains "Date:"

  Scenario: JSON output returns valid structured response
    When I run gtb with "version --output json"
    Then the exit code is 0
    And stdout is valid JSON
    And the JSON field "status" equals "success"
    And the JSON field "command" equals "version"
    And the JSON field "data.version" is not empty

  Scenario: Help flag shows usage
    When I run gtb with "version --help"
    Then the exit code is 0
    And stdout contains "Print version"
