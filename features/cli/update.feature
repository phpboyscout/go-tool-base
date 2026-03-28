@cli @smoke
Feature: CLI Update Command
  The update command manages self-updating to newer versions with
  input validation and helpful usage information.

  Background:
    Given the gtb binary is built

  Scenario: Help flag shows usage and available flags
    When I run gtb with "update --help"
    Then the exit code is 0
    And stdout contains "update to the latest available version"
    And stdout contains "--force"
    And stdout contains "--version"

  Scenario: Invalid semver format returns validation error
    When I run gtb with "update --version bad"
    Then the exit code is not 0
    And stderr contains "invalid version format"
    And stderr contains "expected semVer pattern v0.0.0"

  Scenario: Empty version string with valid format is accepted
    When I run gtb with "update --version v999.999.999"
    Then the exit code is not 0
    And stderr does not contain "invalid version format"
