@cli @smoke
Feature: CLI changelog command
  The `gtb changelog` command displays the version history embedded in the
  binary at build time. It is enabled by default; these smoke scenarios assert
  the command renders the embedded CHANGELOG, filters by latest/version, and
  emits a structured JSON response. The e2e binary embeds a small fixture
  CHANGELOG.md under its assets.

  Background:
    Given the gtb binary is built

  Scenario: changelog help shows usage and filter flags
    When I run gtb with "changelog --help"
    Then the exit code is 0
    And stdout contains "Display the changelog"
    And stdout contains "--latest"
    And stdout contains "--version"

  Scenario: changelog renders the full embedded history
    When I run gtb with "changelog"
    Then the exit code is 0
    And stderr contains "v1.2.0"
    And stderr contains "v1.1.0"

  Scenario: changelog --latest shows only the most recent release
    When I run gtb with "changelog --latest"
    Then the exit code is 0
    And stderr contains "v1.2.0"
    And stderr does not contain "v1.1.0"

  Scenario: changelog emits a structured JSON response
    When I run gtb with "changelog --output json"
    Then the exit code is 0
    And stdout is valid JSON
    And the JSON field "command" equals "changelog"
    And stdout contains "v1.2.0"
