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
    And stdout contains "Print the running binary's version"
    And stdout contains "--check"

  # The following scenarios drive the in-memory stub release source
  # (GTB_E2E_RELEASE_SCENARIO, see cmd/e2e/release_stub.go), which pins a
  # non-development current version so the latest-version check actually
  # runs — hermetically, with no network.

  Scenario: Unreachable release source still prints the local version
    Given I set environment variable "GTB_E2E_RELEASE_SCENARIO" to "unreachable"
    When I run gtb with "version"
    Then the exit code is 0
    And stdout contains "Version:"
    And stderr contains "failed to check latest version"

  Scenario: Explicit check against an unreachable release source fails
    Given I set environment variable "GTB_E2E_RELEASE_SCENARIO" to "unreachable"
    When I run gtb with "version --check"
    Then the exit code is not 0
    And stderr contains "unable to fetch latest version"
