@cli @smoke
Feature: CLI Help
  The root help command lists all available subcommands.

  Background:
    Given the gtb binary is built

  Scenario: Root help lists available commands
    When I run gtb with "--help"
    Then the exit code is 0
    And stdout contains "Available Commands"
    And stdout contains "version"
    And stdout contains "doctor"
    And stdout contains "update"

  Scenario: Unknown command returns error
    When I run gtb with "nonexistent-command"
    Then the exit code is not 0
    And stderr contains "unknown command"
