@cli @smoke
Feature: CLI Telemetry Command
  Users can opt in or out of anonymous usage telemetry.

  Background:
    Given the gtb binary is built

  Scenario: Telemetry is disabled by default
    When I run gtb with "telemetry status"
    Then the exit code is 0
    And stderr contains "disabled"

  Scenario: Enable telemetry
    When I run gtb with "telemetry enable"
    Then the exit code is 0
    And stderr contains "Telemetry enabled"
    When I run gtb with "telemetry status"
    Then the exit code is 0
    And stderr contains "enabled"

  Scenario: Disable telemetry after enabling
    When I run gtb with "telemetry enable"
    And I run gtb with "telemetry disable"
    Then the exit code is 0
    And stderr contains "Telemetry disabled"
    When I run gtb with "telemetry status"
    Then the exit code is 0
    And stderr contains "disabled"

  Scenario: Disable telemetry discards pending events
    When I run gtb with "telemetry enable"
    And I run gtb with "telemetry disable"
    Then stderr contains "All pending events have been discarded"

  Scenario: Status shows machine ID
    When I run gtb with "telemetry status"
    Then the exit code is 0
    And stderr contains "Machine ID:"

  Scenario: Reset clears local data and disables telemetry
    When I run gtb with "telemetry enable"
    And I run gtb with "telemetry reset"
    Then the exit code is 0
    And stderr contains "Local telemetry data cleared"
    When I run gtb with "telemetry status"
    Then stderr contains "disabled"

  Scenario: Help flag shows usage
    When I run gtb with "telemetry --help"
    Then the exit code is 0
    And stdout contains "Manage anonymous usage telemetry"
